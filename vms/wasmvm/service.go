package wasmvm

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ava-labs/gecko/utils/formatting"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/engine/common"
)

// CreateStaticHandlers returns a map where:
// Keys: The path extension for this VM's static API
// Values: The handler for that static API
// We return nil because this VM has no static API
func (vm *VM) CreateStaticHandlers() map[string]*common.HTTPHandler { return nil }

// CreateHandlers returns a map where:
// * keys are API endpoint extensions
// * values are API handlers
// See API documentation for more information
func (vm *VM) CreateHandlers() map[string]*common.HTTPHandler {
	handler := vm.SnowmanVM.NewHandler("wasm", &Service{vm: vm})
	return map[string]*common.HTTPHandler{"": handler}
}

// Service is the API service
type Service struct {
	vm *VM
}

// CreateAccountResponse ...
type CreateAccountResponse struct {
	// A new private key
	Key formatting.CB58 `json:"privateKey"`
}

// CreateAccount returns a new private key
func (s *Service) CreateAccount(_ *http.Request, args *struct{}, response *CreateAccountResponse) error {
	key, err := keyFactory.NewPrivateKey()
	if err != nil {
		return fmt.Errorf("couldn't create new private key: %v", err)
	}
	response.Key = formatting.CB58{Bytes: key.Bytes()}
	return nil
}

// ArgAPI is the API repr of a function argument
type ArgAPI struct {
	Type  string      `json:"type"`
	Value interface{} `json:"value"`
}

// Return argument as its go type
func (arg *ArgAPI) toFnArg() (interface{}, error) {
	switch strings.ToLower(arg.Type) {
	case "int32":
		if valInt32, ok := arg.Value.(int32); ok {
			return valInt32, nil
		}
		if valInt64, ok := arg.Value.(int64); ok {
			return int32(valInt64), nil
		}
		if valFloat32, ok := arg.Value.(float32); ok {
			return int32(valFloat32), nil
		}
		if valFloat64, ok := arg.Value.(float64); ok {
			return int32(valFloat64), nil
		}
		return nil, fmt.Errorf("value '%v' is not convertible to int32", arg.Value)
	case "int64":
		if valInt32, ok := arg.Value.(int32); ok {
			return int64(valInt32), nil
		}
		if valInt64, ok := arg.Value.(int64); ok {
			return valInt64, nil
		}
		if valFloat32, ok := arg.Value.(float32); ok {
			return int64(valFloat32), nil
		}
		if valFloat64, ok := arg.Value.(float64); ok {
			return int64(valFloat64), nil
		}
		return nil, fmt.Errorf("value '%v' is not convertible to int64", arg.Value)
	default:
		return nil, errors.New("arg type must be one of: int32, int64")
	}
}

// InvokeArgs ...
type InvokeArgs struct {
	// Contract to invoke
	ContractID ids.ID `json:"contractID"`
	// Function in contract to invoke
	Function string `json:"function"`
	// Private Key signing the invocation tx
	// This key's address is the "sender" of the tx
	// Must be byte repr. of a SECP256K1R private key
	PrivateKey formatting.CB58 `json:"privateKey"`
	// Integer arguments to the function
	Args []ArgAPI `json:"args"`
	// Byte arguments to the function
	ByteArgs formatting.CB58 `json:"byteArgs"`
}

func (args *InvokeArgs) validate() error {
	if args.ContractID.Equals(ids.Empty) {
		return errors.New("contractID not specified")
	}
	if args.Function == "" {
		return errors.New("function not specified")
	}
	return nil
}

// InvokeResponse ...
type InvokeResponse struct {
	TxID ids.ID `json:"txID"`
}

// Invoke ...
func (s *Service) Invoke(_ *http.Request, args *InvokeArgs, response *InvokeResponse) error {
	s.vm.Ctx.Log.Debug("in invoke")
	if err := args.validate(); err != nil {
		return fmt.Errorf("arguments failed validation: %v", err)
	}

	fnArgs := make([]interface{}, len(args.Args))
	var err error
	for i, arg := range args.Args {
		fnArgs[i], err = arg.toFnArg()
		if err != nil {
			return fmt.Errorf("couldn't parse arg '%+v': %s", arg, err)
		}
	}

	privateKey, err := keyFactory.ToPrivateKey(args.PrivateKey.Bytes)
	if err != nil {
		return fmt.Errorf("couldn't parse 'privateKey' to a SECP256K1R private key: %v", err)
	}

	tx, err := s.vm.newInvokeTx(args.ContractID, args.Function, fnArgs, args.ByteArgs.Bytes, privateKey)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %s", err)
	}

	// Add tx to mempool
	s.vm.mempool = append(s.vm.mempool, tx)
	s.vm.NotifyBlockReady()

	response.TxID = tx.ID()
	return nil
}

// CreateContractArgs ...
type CreateContractArgs struct {
	// The byte representation of the contract.
	// Must be a valid wasm file.
	Contract formatting.CB58 `json:"contract"`

	// Byte repr. of the private key of the sender of this tx
	// Should be a SECP256K1R private key
	PrivateKey formatting.CB58 `json:"privateKey"`
}

// CreateContract creates a new contract
// The contract's ID is the ID of the tx that creates it, which is returned by this method
func (s *Service) CreateContract(_ *http.Request, args *CreateContractArgs, response *ids.ID) error {
	s.vm.Ctx.Log.Debug("in createContract")

	privateKey, err := keyFactory.ToPrivateKey(args.PrivateKey.Bytes)
	if err != nil {
		return fmt.Errorf("couldn't parse 'privateKey' to a SECP256K1R private key: %v", err)
	}

	tx, err := s.vm.newCreateContractTx(args.Contract.Bytes, privateKey)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %v", err)
	}

	// Add tx to mempool
	s.vm.mempool = append(s.vm.mempool, tx)
	s.vm.NotifyBlockReady()

	*response = tx.ID()
	return nil

}

// GetTxArgs ...
type GetTxArgs struct {
	ID ids.ID `json:"id"`
}

// GetTxResponse ...
type GetTxResponse struct {
	Tx *txReturnValue `json:"tx"`
}

// GetTx returns a tx by its ID
func (s *Service) GetTx(_ *http.Request, args *GetTxArgs, response *GetTxResponse) error {
	tx, err := s.vm.getTx(s.vm.DB, args.ID)
	if err != nil {
		return fmt.Errorf("couldn't find tx with ID %s", args.ID)
	}
	response.Tx = tx
	return nil
}