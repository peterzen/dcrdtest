module github.com/decred/dcrd/hdkeychain/v3

go 1.13

require (
	github.com/decred/base58 v1.0.3
	github.com/decred/dcrd/chaincfg/v3 v3.0.0
	github.com/decred/dcrd/crypto/blake256 v1.0.0
	github.com/decred/dcrd/crypto/ripemd160 v1.0.1
	github.com/decred/dcrd/dcrec v1.0.0
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.0-20210127014238-b33b46cf1a24
	github.com/decred/dcrd/dcrutil/v4 v4.0.0
)

replace (
	github.com/decred/dcrd/dcrec/secp256k1/v4 => ../dcrec/secp256k1
	github.com/decred/dcrd/dcrutil/v4 => ../dcrutil
)
