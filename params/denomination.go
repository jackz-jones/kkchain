package params

// These are the multipliers for king denominations.
// Example: To get the wei value of an amount in 'douglas', use
//
//    new(big.Int).Mul(value, big.NewInt(params.Douglas))
//
const (
	Wei     = 1
	Ada     = 1e3
	Babbage = 1e6
	Shannon = 1e9
	Szabo   = 1e12
	Finney  = 1e15
	King    = 1e18
)
