package identifier

import (
	"fmt"
	"math/rand"
	"time"
)

const MinIdentifier uint64 = 0x000000000000
const MaxIdentifier uint64 = 0xFFFFFFFFFFFF
const MinValidIdentifier uint64 = MinIdentifier + 1
const MaxValidIdentifier uint64 = MaxIdentifier - 1

var r = rand.New(rand.NewSource(time.Now().UnixNano()))

func Seed(seed int64) {
	r = rand.New(rand.NewSource(seed))
}

func NewFromInteger(value uint64) (string, error) {
	if value == 0 || value == MaxIdentifier {
		return "", fmt.Errorf("%d is a special value that cannot be used", value)
	}
	if value > MaxIdentifier {
		return "", fmt.Errorf("%d exceeds maximum value %d", value, MaxIdentifier)
	}
	return fmt.Sprintf("%012x", value), nil
}

func NewFromRandom() (string, error) {
	n := uint64(r.Int63n(int64(MaxValidIdentifier)) + int64(MinValidIdentifier))
	return NewFromInteger(n)
}
