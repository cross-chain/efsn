package common

import (
	"encoding/json"
	"fmt"
	"math/big"
)

// SystemAssetID wacom
var SystemAssetID = HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")

// OwnerUSANAssetID wacom
var OwnerUSANAssetID = HexToHash("0xfffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe")

var (
	// FSNCallAddress wacom
	FSNCallAddress = HexToAddress("0xffffffffffffffffffffffffffffffffffffffff")

	// TicketLogAddress wacom (deprecated)
	TicketLogAddress = HexToAddress("0xfffffffffffffffffffffffffffffffffffffffe")

	// NotationKeyAddress wacom
	NotationKeyAddress = HexToAddress("0xfffffffffffffffffffffffffffffffffffffffd")

	// AssetKeyAddress wacom
	AssetKeyAddress = HexToAddress("0xfffffffffffffffffffffffffffffffffffffffc")

	// TicketKeyAddress wacom
	TicketKeyAddress = HexToAddress("0xfffffffffffffffffffffffffffffffffffffffb")

	// SwapKeyAddress wacom
	SwapKeyAddress = HexToAddress("0xfffffffffffffffffffffffffffffffffffffffa")

	// MultiSwapKeyAddress wacom
	MultiSwapKeyAddress = HexToAddress("0xfffffffffffffffffffffffffffffffffffffff9")

	// ReportIllegalAddress wacom
	ReportKeyAddress = HexToAddress("0xfffffffffffffffffffffffffffffffffffffff8")

	// FSNContractAddress wacom
	FSNContractAddress = HexToAddress("0xfffffffffffffffffffffffffffffffffffffff7")
)

func (addr Address) IsSpecialKeyAddress() bool {
	return addr == TicketKeyAddress ||
		addr == NotationKeyAddress ||
		addr == AssetKeyAddress ||
		addr == SwapKeyAddress ||
		addr == MultiSwapKeyAddress ||
		addr == ReportKeyAddress
}

var (
	// AutoBuyTicket wacom
	AutoBuyTicket = false
	// AutoBuyTicketChan wacom
	AutoBuyTicketChan = make(chan int, 10)

	// ReportIllegal wacom
	ReportIllegalChan = make(chan []byte)
)

// FSNCallFunc wacom
type FSNCallFunc uint8

const (
	// GenNotationFunc wacom
	GenNotationFunc = iota
	// GenAssetFunc wacom
	GenAssetFunc
	// SendAssetFunc wacom
	SendAssetFunc
	// TimeLockFunc wacom
	TimeLockFunc
	// BuyTicketFunc wacom
	BuyTicketFunc
	// OldAssetValueChangeFunc wacom (deprecated)
	OldAssetValueChangeFunc
	// MakeSwapFunc wacom
	MakeSwapFunc
	// RecallSwapFunc wacom
	RecallSwapFunc
	// TakeSwapFunc wacom
	TakeSwapFunc
	// EmptyFunc wacom
	EmptyFunc
	// MakeSwapFuncExt wacom
	MakeSwapFuncExt
	// TakeSwapFuncExt wacom
	TakeSwapFuncExt
	// AssetValueChangeFunc wacom
	AssetValueChangeFunc
	// MakeMultiSwapFunc wacom
	MakeMultiSwapFunc
	// RecallMultiSwapFunc wacom
	RecallMultiSwapFunc
	// TakeMultiSwapFunc wacom
	TakeMultiSwapFunc
	// ReportIllegalFunc wacom
	ReportIllegalFunc
)

func (f FSNCallFunc) Name() string {
	switch f {
	case GenNotationFunc:
		return "GenNotationFunc"
	case GenAssetFunc:
		return "GenAssetFunc"
	case SendAssetFunc:
		return "SendAssetFunc"
	case TimeLockFunc:
		return "TimeLockFunc"
	case BuyTicketFunc:
		return "BuyTicketFunc"
	case OldAssetValueChangeFunc:
		return "OldAssetValueChangeFunc"
	case MakeSwapFunc:
		return "MakeSwapFunc"
	case RecallSwapFunc:
		return "RecallSwapFunc"
	case TakeSwapFunc:
		return "TakeSwapFunc"
	case EmptyFunc:
		return "EmptyFunc"
	case MakeSwapFuncExt:
		return "MakeSwapFuncExt"
	case TakeSwapFuncExt:
		return "TakeSwapFuncExt"
	case AssetValueChangeFunc:
		return "AssetValueChangeFunc"
	case MakeMultiSwapFunc:
		return "MakeMultiSwapFunc"
	case RecallMultiSwapFunc:
		return "RecallMultiSwapFunc"
	case TakeMultiSwapFunc:
		return "TakeMultiSwapFunc"
	case ReportIllegalFunc:
		return "ReportIllegalFunc"
	}
	return "Unkonwn"
}

func IsFsnContractCall(to *Address) bool {
	return to != nil && *to == FSNContractAddress
}

func IsFsnCall(to *Address) bool {
	return to != nil && *to == FSNCallAddress
}

func GetFsnCallFee(funcType FSNCallFunc) *big.Int {
	fee := big.NewInt(0)
	switch funcType {
	case GenNotationFunc:
		fee = big.NewInt(100000000000000000) // 0.1 FSN
	case GenAssetFunc:
		fee = big.NewInt(10000000000000000) // 0.01 FSN
	case MakeSwapFunc, MakeSwapFuncExt, MakeMultiSwapFunc:
		fee = big.NewInt(1000000000000000) // 0.001 FSN
	case TimeLockFunc:
		fee = big.NewInt(1000000000000000) // 0.001 FSN
	}
	return fee
}

// ToAsset wacom
func (p *GenAssetParam) ToAsset() Asset {
	return Asset{
		Name:        p.Name,
		Symbol:      p.Symbol,
		Decimals:    p.Decimals,
		Total:       p.Total,
		CanChange:   p.CanChange,
		Description: p.Description,
	}
}

// Asset wacom
type Asset struct {
	ID          Hash
	Owner       Address
	Name        string
	Symbol      string
	Decimals    uint8
	Total       *big.Int `json:",string"`
	CanChange   bool
	Description string
}

func (u *Asset) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		ID          Hash
		Owner       Address
		Name        string
		Symbol      string
		Decimals    uint8
		Total       string
		CanChange   bool
		Description string
	}{
		ID:          u.ID,
		Owner:       u.Owner,
		Name:        u.Name,
		Symbol:      u.Symbol,
		Decimals:    u.Decimals,
		Total:       u.Total.String(),
		CanChange:   u.CanChange,
		Description: u.Description,
	})
}

// SystemAsset wacom
var SystemAsset = Asset{
	Name:        "Fusion",
	Symbol:      "FSN",
	Decimals:    18,
	Total:       new(big.Int).Mul(big.NewInt(81920000), big.NewInt(1000000000000000000)),
	ID:          SystemAssetID,
	Description: "https://fusion.org",
}

// Swap wacom
type Swap struct {
	ID            Hash
	Owner         Address
	FromAssetID   Hash
	FromStartTime uint64
	FromEndTime   uint64
	MinFromAmount *big.Int `json:",string"`
	ToAssetID     Hash
	ToStartTime   uint64
	ToEndTime     uint64
	MinToAmount   *big.Int `json:",string"`
	SwapSize      *big.Int `json:",string"`
	Targes        []Address
	Time          *big.Int // Provides information for TIME
	Description   string
	Notation      uint64
}

// MultiSwap wacom
type MultiSwap struct {
	ID            Hash
	Owner         Address
	FromAssetID   []Hash
	FromStartTime []uint64
	FromEndTime   []uint64
	MinFromAmount []*big.Int `json:",string"`
	ToAssetID     []Hash
	ToStartTime   []uint64
	ToEndTime     []uint64
	MinToAmount   []*big.Int `json:",string"`
	SwapSize      *big.Int   `json:",string"`
	Targes        []Address
	Time          *big.Int // Provides information for TIME
	Description   string
	Notation      uint64
}

func (s *MultiSwap) IsMortgage() bool {
	return IsMortgage(s.Targes)
}

func (s *MultiSwap) GetInterest() *big.Int {
	return GetMortgageInterest(s.Targes)
}

func (s *MultiSwap) GetLender() Address {
	if s.IsMortgage() {
		return s.Targes[len(s.Targes)-1]
	}
	return (Address{})
}

func (s *MultiSwap) GetLastToEndTime() uint64 {
	lastEnd := uint64(0)
	for _, endtime := range s.ToEndTime {
		if endtime > lastEnd {
			lastEnd = endtime
		}
	}
	return lastEnd
}

// use empty address to separate mortgage params from target addresses
// eg. [param1, param2, ..., empty, target1, target2, ...]
func IsMortgage(targets []Address) bool {
	for _, addr := range targets {
		if addr == (Address{}) {
			return true
		}
	}
	return false
}

func GetMortgageInterest(targets []Address) *big.Int {
	if IsMortgage(targets) {
		return targets[0].Big()
	}
	return big.NewInt(0)
}

func CheckSwapTargets(targets []Address, addr Address) error {
	if len(targets) == 0 {
		return nil
	}
	sepIndex := -1
	for i := len(targets) - 1; i >= 0; i-- {
		if targets[i] == (Address{}) {
			sepIndex = i
			break
		}
	}
	realTargets := targets[sepIndex+1:]
	if len(realTargets) == 0 {
		return nil
	}
	for _, target := range realTargets {
		if addr == target {
			return nil
		}
	}
	return fmt.Errorf("swap taker does not match the specified targets")
}

// KeyValue wacom
type KeyValue struct {
	Key   string
	Value interface{}
}

// NewKeyValue wacom
func NewKeyValue(name string, v interface{}) *KeyValue {
	return &KeyValue{Key: name, Value: v}
}
