# batch-query

Batch-query is a library with utilities that can query multiple values on the Ethereum blockchain at once.
It's intended to reduce the RPC overhead associated with running multiple `eth_call` invocations simultaneously.  
It comes with two main structs:

- `BalanceBatcher` can query the ETH balances of multiple addresses within a single call to an Execution Client. It uses the contract from [https://github.com/wbobeirne/eth-balance-checker](https://github.com/wbobeirne/eth-balance-checker).
- `MultiCaller` can run multiple contract calls (`eth_call`) within a single call to an Execution Client. It uses the v2 Multicaller contract from [https://github.com/makerdao/multicall](https://github.com/makerdao/multicall).
