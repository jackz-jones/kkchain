package common

import (
	"time"
)

var (
	MaxHashFetch    = 512      // Amount of hashes to be fetched per retrieval request
	MaxBlockFetch   = 128      // Amount of blocks to be fetched per retrieval request
	MaxBlockPerSync = 128 * 20 // FIXME: Amount of blocks to be fetched per seesion
	MaxHeaderFetch  = 192      // Amount of block headers to be fetched per retrieval request
	MaxSkeletonSize = 128      // Number of header fetches to need for a skeleton assembly
	MaxBodyFetch    = 128      // Amount of block bodies to be fetched per retrieval request

	RTTMinEstimate   = 2 * time.Second  // Minimum round-trip time to target for download requests
	RTTMaxEstimate   = 20 * time.Second // Maximum round-trip time to target for download requests
	RTTMinConfidence = 0.1              // Worse confidence factor in our estimated RTT value
	RTTScaling       = 3                // Constant scaling factor for RTT -> TTL conversion
	TTLLimit         = time.Minute      // Maximum TTL allowance to prevent reaching crazy timeouts

	FSMinFullBlocks  = 64              // Number of blocks to retrieve fully even in fast sync
)

