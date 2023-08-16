package batchquery

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/sync/errgroup"
)

const (
	// The contract and ABI this uses comes from: https://github.com/wbobeirne/eth-balance-checker
	balanceBatcherAbiString string = "[{\"constant\":true,\"inputs\":[{\"name\":\"user\",\"type\":\"address\"},{\"name\":\"token\",\"type\":\"address\"}],\"name\":\"tokenBalance\",\"outputs\":[{\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[{\"name\":\"users\",\"type\":\"address[]\"},{\"name\":\"tokens\",\"type\":\"address[]\"}],\"name\":\"balances\",\"outputs\":[{\"name\":\"\",\"type\":\"uint256[]\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"payable\":true,\"stateMutability\":\"payable\",\"type\":\"fallback\"}]"
)

// ABI cache
var balanceBatcherAbi *abi.ABI

// This struct can query the ETH balances of multiple addresses within a single call to an Execution Client.
// It is useful if you need to query the balance of many addresses, because batching them reduces RPC overhead.
type BalanceBatcher struct {
	// The number of addresses to query within a single call
	BalanceBatchSize int

	// The number of calls to run simultaneously, if the list of addresses is too large for a single call
	ThreadLimit int

	// The Execution client binding
	client IContractCaller

	// Address of the balance batcher contract
	contractAddress common.Address

	// ABI for the balance batcher contract
	abi *abi.ABI
}

// Creates a new BalanceBatcher instance
func NewBalanceBatcher(client IContractCaller, address common.Address, balanceBatchSize int, threadLimit int) (*BalanceBatcher, error) {
	if balanceBatcherAbi == nil {
		abi, err := abi.JSON(strings.NewReader(balanceBatcherAbiString))
		if err != nil {
			return nil, err
		}
		balanceBatcherAbi = &abi
	}

	return &BalanceBatcher{
		client:           client,
		contractAddress:  address,
		abi:              balanceBatcherAbi,
		BalanceBatchSize: balanceBatchSize,
		ThreadLimit:      threadLimit,
	}, nil
}

// Retrieves the ETH balance for a list of addresses. The order of the resulting array corresponds to the order of the provided addresses.
func (b *BalanceBatcher) GetEthBalances(addresses []common.Address, opts *bind.CallOpts) ([]*big.Int, error) {
	count := len(addresses)
	balances := make([]*big.Int, count)
	var wg errgroup.Group
	wg.SetLimit(b.ThreadLimit)

	// Run the getters in batches
	for i := 0; i < count; i += b.BalanceBatchSize {
		i := i
		max := i + b.BalanceBatchSize
		if max > count {
			max = count
		}

		wg.Go(func() error {
			subAddresses := addresses[i:max]
			tokens := []common.Address{
				{}, // Empty token for ETH balance
			}
			callData, err := b.abi.Pack("balances", subAddresses, tokens)
			if err != nil {
				return fmt.Errorf("error creating calldata for balances: %w", err)
			}

			// Get the balances
			var blockNumber *big.Int
			if opts != nil {
				blockNumber = opts.BlockNumber
			}
			response, err := b.client.CallContract(context.Background(), ethereum.CallMsg{To: &b.contractAddress, Data: callData}, blockNumber)
			if err != nil {
				return fmt.Errorf("error calling balances: %w", err)
			}

			// Sanity checking and verification
			var subBalances []*big.Int
			err = b.abi.UnpackIntoInterface(&subBalances, "balances", response)
			if err != nil {
				return fmt.Errorf("error unpacking balances response: %w", err)
			}
			if len(subBalances) != len(subAddresses) {
				return fmt.Errorf("received %d balances which mismatches query batch size %d", len(subBalances), len(subAddresses))
			}
			for j, balance := range subBalances {
				if balance == nil {
					return fmt.Errorf("received nil balance for address %s", subAddresses[j].String())
				}
				balances[i+j] = balance
			}

			return nil
		})
	}

	err := wg.Wait()
	if err != nil {
		return nil, fmt.Errorf("error getting balances: %w", err)
	}

	return balances, nil
}
