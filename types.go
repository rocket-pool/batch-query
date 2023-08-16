package batchquery

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
)

// This is an Execution client binding that can call a contract function
type IContractCaller interface {
	// Calls a contract function, typically using eth_call
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
}
