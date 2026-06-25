# indexer

Index UTXOs and balances per address.

### Balance Cache

The `-cache-balances` flag maintains a `balance` table for faster
`/balance` responses. This cache is Postgres-only because it relies on `NUMERIC` columns for large aggregate balances.

Example:

```sh
indexer \
  -dburl='postgres://indexer:password@localhost:5432/indexer?sslmode=disable' \
  -cache-balances=true
```