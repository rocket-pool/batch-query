/*
* This code was derived from https://github.com/depocket/multicall-go
 */

package batchquery

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
)

const (
	// This is the ABI for Multicall v2: https://github.com/makerdao/multicall
	multicallAbiString string = "[{\"inputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"target\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"callData\",\"type\":\"bytes\"}],\"internalType\":\"struct Multicall2.Call[]\",\"name\":\"calls\",\"type\":\"tuple[]\"}],\"name\":\"aggregate\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"},{\"internalType\":\"bytes[]\",\"name\":\"returnData\",\"type\":\"bytes[]\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"target\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"callData\",\"type\":\"bytes\"}],\"internalType\":\"struct Multicall2.Call[]\",\"name\":\"calls\",\"type\":\"tuple[]\"}],\"name\":\"blockAndAggregate\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"blockHash\",\"type\":\"bytes32\"},{\"components\":[{\"internalType\":\"bool\",\"name\":\"success\",\"type\":\"bool\"},{\"internalType\":\"bytes\",\"name\":\"returnData\",\"type\":\"bytes\"}],\"internalType\":\"struct Multicall2.Result[]\",\"name\":\"returnData\",\"type\":\"tuple[]\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"name\":\"getBlockHash\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"blockHash\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getBlockNumber\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getCurrentBlockCoinbase\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"coinbase\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getCurrentBlockDifficulty\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"difficulty\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getCurrentBlockGasLimit\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"gaslimit\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getCurrentBlockTimestamp\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"timestamp\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"getEthBalance\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getLastBlockHash\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"blockHash\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bool\",\"name\":\"requireSuccess\",\"type\":\"bool\"},{\"components\":[{\"internalType\":\"address\",\"name\":\"target\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"callData\",\"type\":\"bytes\"}],\"internalType\":\"struct Multicall2.Call[]\",\"name\":\"calls\",\"type\":\"tuple[]\"}],\"name\":\"tryAggregate\",\"outputs\":[{\"components\":[{\"internalType\":\"bool\",\"name\":\"success\",\"type\":\"bool\"},{\"internalType\":\"bytes\",\"name\":\"returnData\",\"type\":\"bytes\"}],\"internalType\":\"struct Multicall2.Result[]\",\"name\":\"returnData\",\"type\":\"tuple[]\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bool\",\"name\":\"requireSuccess\",\"type\":\"bool\"},{\"components\":[{\"internalType\":\"address\",\"name\":\"target\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"callData\",\"type\":\"bytes\"}],\"internalType\":\"struct Multicall2.Call[]\",\"name\":\"calls\",\"type\":\"tuple[]\"}],\"name\":\"tryBlockAndAggregate\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"blockHash\",\"type\":\"bytes32\"},{\"components\":[{\"internalType\":\"bool\",\"name\":\"success\",\"type\":\"bool\"},{\"internalType\":\"bytes\",\"name\":\"returnData\",\"type\":\"bytes\"}],\"internalType\":\"struct Multicall2.Result[]\",\"name\":\"returnData\",\"type\":\"tuple[]\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]"
)

// ABI cache
var multicallAbi abi.ABI
var mcOnce sync.Once

// A single contract call wrapper
type Call struct {
	// The contract address of the target to run the call on
	Target common.Address `json:"target"`

	// Packed call data to be passed to the function as input
	CallData []byte `json:"callData"`

	// The name of the method being called (for debugging only)
	Method string `json:"-"`

	// Function to generate the call data
	PackFunc func() ([]byte, error) `json:"-"`

	// Function to generate the output from the response
	UnpackFunc func([]byte) error `json:"-"`
}

// The response from a contract call invocation
type CallResponse struct {
	// Whether or not the particular call worked
	Status bool `json:"success"`

	// The return data for the call
	ReturnData []byte `json:"returnData"`
}

// MultiCaller is capable of batching multiple arbitrary contract calls into one and executing them at the same time within a single `eth_call` to the client.
// It uses MakerDAO's Multicall v2 contract under the hood.
type MultiCaller struct {
	// The execution client
	client IContractCaller

	// The multicall v2 contract address
	contractAddress common.Address

	// The collection of calls to batch and execute during the next FlexibleCall()
	calls []Call
}

// Creates a new MultiCaller instance with the provided execution client and address of the multicaller contract
func NewMultiCaller(client IContractCaller, multicallerAddress common.Address) (*MultiCaller, error) {

	var err error
	mcOnce.Do(func() {
		var parsedAbi abi.ABI
		parsedAbi, err = abi.JSON(strings.NewReader(multicallAbiString))
		if err == nil {
			multicallAbi = parsedAbi
		}
	})
	if err != nil {
		return nil, err
	}

	return &MultiCaller{
		client:          client,
		contractAddress: multicallerAddress,
		calls:           []Call{},
	}, nil
}

// Adds a contract call to the batch of calls to query during the next run
func (mc *MultiCaller) AddCall(contractAddress common.Address, abi *abi.ABI, output any, method string, args ...any) {
	call := Call{
		Target: contractAddress,
		Method: method,
		PackFunc: func() ([]byte, error) {
			callData, err := abi.Pack(method, args...)
			if err != nil {
				return nil, fmt.Errorf("error packing data for call [%s] on contract %s: %w", method, contractAddress.Hex(), err)
			}
			return callData, nil
		},
		UnpackFunc: func(rawData []byte) error {
			return abi.UnpackIntoInterface(output, method, rawData)
		},
	}
	mc.calls = append(mc.calls, call)
}

// Invokes all of the previously batched up contract calls in a single call.
// If requireSuccess is true, a single error will cause all of the calls to fail.
// If false, the calls can run independently and you will be given a list of resulting success or fail flags for each call.
// Upon completion, the internal list of batched up contract calls will be cleared.
func (mc *MultiCaller) FlexibleCall(requireSuccess bool, opts *bind.CallOpts) ([]bool, error) {
	if len(mc.calls) == 0 {
		return []bool{}, nil
	}
	res := make([]bool, len(mc.calls))

	// Create the CallData for each call
	for i, call := range mc.calls {
		callData, err := call.PackFunc()
		if err != nil {
			return nil, err
		}
		mc.calls[i].CallData = callData
	}

	// Prep the multicall args
	callData, err := multicallAbi.Pack("tryAggregate", requireSuccess, mc.calls)
	if err != nil {
		return nil, fmt.Errorf("error packing aggregated call data: %w", err)
	}

	// Invoke the multicall function
	var blockNumber *big.Int
	if opts != nil {
		blockNumber = opts.BlockNumber
	}
	resp, err := mc.client.CallContract(context.Background(), ethereum.CallMsg{To: &mc.contractAddress, Data: callData}, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("error calling multicall contract: %w", err)
	}

	// Unpack the multicall output
	results := make([]CallResponse, len(mc.calls))
	err = multicallAbi.UnpackIntoInterface(&results, "tryAggregate", resp)
	if err != nil {
		return nil, fmt.Errorf("error unpacking aggregated response data: %w", err)
	}

	// Unpack the individual call results per function
	for i, c := range mc.calls {
		callSuccess := results[i].Status
		if callSuccess {
			err := c.UnpackFunc(results[i].ReturnData)
			if err != nil {
				mc.calls = []Call{}
				return nil, fmt.Errorf("error unpacking response for contract %s, method %s: %w", c.Target.Hex(), c.Method, err)
			}
		}
		res[i] = callSuccess
	}

	// Reset the call list
	mc.calls = []Call{}
	return res, err
}
