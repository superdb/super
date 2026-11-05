package vhash

import "hash/maphash"

var seed = maphash.MakeSeed()

// fmix64 is the 64-bit finalizer from MurmurHash3.  It is used to hash
// fixed-width integer values so that the low bits (used for the slot offset)
// and the high bits (used for the salt) are both well distributed.
func fmix64(h uint64) uint64 {
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return h
}

// combine folds the hash of the next key position into the running hash h.
// h is scrambled before the XOR so the result depends on key order, and next
// is already a well-mixed hash, so no further finalization is needed.
func combine(h, next uint64) uint64 {
	h ^= h >> 32
	h *= 0xd6e8feb86659fd93
	return h ^ next
}
