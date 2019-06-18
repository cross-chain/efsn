package datong

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/FusionFoundation/efsn/accounts"
	"github.com/FusionFoundation/efsn/common"
	"github.com/FusionFoundation/efsn/consensus"
	"github.com/FusionFoundation/efsn/core/rawdb"
	"github.com/FusionFoundation/efsn/core/state"
	"github.com/FusionFoundation/efsn/core/types"
	"github.com/FusionFoundation/efsn/crypto"
	"github.com/FusionFoundation/efsn/crypto/sha3"
	"github.com/FusionFoundation/efsn/ethdb"
	"github.com/FusionFoundation/efsn/log"
	"github.com/FusionFoundation/efsn/params"
	"github.com/FusionFoundation/efsn/rlp"
	"github.com/FusionFoundation/efsn/rpc"

	cmath "github.com/FusionFoundation/efsn/common/math"
)

const (
	delayTimeModifier    = 15 // adjust factor
	adjustIntervalBlocks = 10 // adjust delay time by blocks

	maxNumberOfDeletedTickets = 7 // maximum number of tickets to be deleted because not mining block in time
)

var (
	errUnknownBlock = errors.New("unknown block")

	errCoinbase = errors.New("error coinbase")

	errMissingVanity = errors.New("extra-data 32 byte vanity prefix missing")

	errMissingSignature = errors.New("extra-data 65 byte suffix signature missing")

	errInvalidUncleHash = errors.New("non empty uncle hash")

	errUnauthorized = errors.New("unauthorized")
)

// SignerFn is a signer callback function to request a hash to be signed by a
// backing account.
type SignerFn func(accounts.Account, []byte) ([]byte, error)

var (
	extraVanity         = 32
	extraSeal           = 65
	MinBlockTime int64  = 7   // 7 seconds
	maxBlockTime uint64 = 600 // 10 minutes

	emptyUncleHash = types.CalcUncleHash(nil)
)

// DaTong wacom
type DaTong struct {
	config     *params.DaTongConfig
	db         ethdb.Database
	stateCache state.Database

	signer common.Address
	signFn SignerFn
	lock   sync.RWMutex
}

// New wacom
func New(config *params.DaTongConfig, db ethdb.Database) *DaTong {
	return &DaTong{
		config:     config,
		db:         db,
		stateCache: nil,
	}
}

// Authorize wacom
func (dt *DaTong) Authorize(signer common.Address, signFn SignerFn) {
	dt.lock.Lock()
	defer dt.lock.Unlock()
	dt.signer = signer
	dt.signFn = signFn
}

// Author retrieves the Ethereum address of the account that minted the given
// block, which may be different from the header's coinbase if a consensus
// engine is based on signatures.
func (dt *DaTong) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

// VerifyHeader checks whether a header conforms to the consensus rules of the
// stock Ethereum ethash engine.
func (dt *DaTong) verifyHeader(chain consensus.ChainReader, header *types.Header, seal bool, parents []*types.Header) error {
	if header.Number == nil || header.Number.Sign() == 0 {
		return errUnknownBlock
	}
	// Checkpoint blocks need to enforce zero beneficiary
	if header.Coinbase == (common.Address{}) {
		return errCoinbase
	}
	if len(header.Extra) < extraVanity {
		return errMissingVanity
	}
	if len(header.Extra) < extraVanity+extraSeal {
		return errMissingSignature
	}
	if header.UncleHash != emptyUncleHash {
		return errInvalidUncleHash
	}
	if header.Time.Cmp(big.NewInt(time.Now().Unix())) > 0 {
		return consensus.ErrFutureBlock
	}
	// verify Ancestor
	parent, err := getParent(chain, header, parents)
	if err != nil {
		return err
	}
	// verify header time
	if header.Time.Int64()-parent.Time.Int64() < MinBlockTime {
		return fmt.Errorf("block %v header.Time:%v < parent.Time:%v + %v Second",
			header.Number, header.Time.Int64(), parent.Time.Int64(), MinBlockTime)

	}
	// verify signature
	signature := header.Extra[len(header.Extra)-extraSeal:]
	pubkey, err := crypto.Ecrecover(sigHash(header).Bytes(), signature)
	if err != nil {
		return err
	}
	var signer common.Address
	copy(signer[:], crypto.Keccak256(pubkey[1:])[12:])
	if header.Coinbase != signer {
		return errors.New("Coinbase is not the signer")
	}
	// check block time
	if err = dt.checkBlockTime(chain, header, parent); err != nil {
		return err
	}
	return dt.verifySeal(chain, header, parent)
}

// VerifyHeader checks whether a header conforms to the consensus rules of the
// stock Ethereum ethash engine.
func (dt *DaTong) VerifyHeader(chain consensus.ChainReader, header *types.Header, seal bool) error {
	return dt.verifyHeader(chain, header, seal, glb_parents)
}

// VerifyHeaders is similar to VerifyHeader, but verifies a batch of headers
// concurrently. The method returns a quit channel to abort the operations and
// a results channel to retrieve the async verifications.
func (dt *DaTong) VerifyHeaders(chain consensus.ChainReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error) {
	abort := make(chan struct{})
	results := make(chan error, len(headers))
	go func() {
		for i, header := range headers {
			err := dt.verifyHeader(chain, header, seals[i], headers[:i])
			select {
			case <-abort:
				return
			case results <- err:
			}
		}
	}()

	return abort, results
}

// VerifyUncles verifies that the given block's uncles conform to the consensus
// rules of the stock Ethereum ethash engine.
func (dt *DaTong) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	return nil
}

// VerifySeal implements consensus.Engine, checking whether the signature contained
// in the header satisfies the consensus protocol requirements.
func (c *DaTong) VerifySeal(chain consensus.ChainReader, header *types.Header) error {
	return c.verifySeal(chain, header, nil)
}

var glb_parents []*types.Header

func SetHeaders(parents []*types.Header) {
	glb_parents = parents
}

func getParent(chain consensus.ChainReader, header *types.Header, parents []*types.Header) (*types.Header, error) {
	number := header.Number.Uint64()
	var parent *types.Header
	if parents != nil && len(parents) > 0 {
		parent = parents[len(parents)-1]
	} else {
		parent = chain.GetHeader(header.ParentHash, number-1)
	}
	if parent == nil || parent.Number.Uint64() != number-1 || parent.Hash() != header.ParentHash {
		return nil, consensus.ErrUnknownAncestor
	}
	return parent, nil
}

// VerifySeal checks whether the crypto seal on a header is valid according to
// the consensus rules of the given engine.
func (dt *DaTong) verifySeal(chain consensus.ChainReader, header *types.Header, parent *types.Header) error {
	// verify ticket
	snap, err := newSnapshotWithData(getSnapDataByHeader(header))
	if err != nil {
		return err
	}
	ticketID := snap.GetVoteTicket()
	// verify ticket: list squence, ID , ticket Info, difficulty
	diff, tk, listSq, _, errv := dt.calcBlockDifficulty(chain, header, parent)
	if errv != nil {
		return errv
	}
	// verify ticket with signer
	if tk.Owner() != header.Coinbase {
		return errors.New("Coinbase is not the voted ticket owner")
	}
	// check ticket ID
	if tk.ID != ticketID {
		return fmt.Errorf("verifySeal ticketID mismatch, have %v, want %v", ticketID.String(), tk.ID.String())
	}
	// check ticket info
	errt := dt.checkTicketInfo(header, tk)
	if errt != nil {
		return errt
	}
	// check ticket order
	if header.Nonce != types.EncodeNonce(listSq) {
		return fmt.Errorf("verifySeal ticket order mismatch, have %v, want %v", header.Nonce.Uint64(), listSq)
	}

	// check difficulty
	if diff.Cmp(header.Difficulty) != 0 {
		return fmt.Errorf("verifySeal difficulty mismatch, have %v, want %v", header.Difficulty, diff)
	}

	return nil
}

// Prepare initializes the consensus fields of a block header according to the
// rules of a particular engine. The changes are executed inline.
func (dt *DaTong) Prepare(chain consensus.ChainReader, header *types.Header) error {
	number := header.Number.Uint64()
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	if len(header.Extra) < extraVanity {
		header.Extra = append(header.Extra, bytes.Repeat([]byte{0x00}, extraVanity-len(header.Extra))...)
	}
	header.Extra = header.Extra[:extraVanity]
	header.Extra = append(header.Extra, make([]byte, extraSeal)...)
	difficulty, _, order, _, err := dt.calcBlockDifficulty(chain, header, parent)
	if err != nil {
		return err
	}
	header.Nonce = types.EncodeNonce(order)
	header.Difficulty = difficulty
	// adjust block time if illegal
	if order > 0 {
		recvTime := header.Time.Int64() - parent.Time.Int64()
		maxDelaySeconds := int64(maxBlockTime + dt.config.Period)
		if recvTime < maxDelaySeconds {
			expectTime := int64(dt.config.Period + order*delayTimeModifier)
			if recvTime < expectTime {
				if expectTime > maxDelaySeconds {
					expectTime = maxDelaySeconds
				}
				header.Time = big.NewInt(parent.Time.Int64() + expectTime)
			}
		}
	}
	return nil
}

type DisInfo struct {
	tk  *common.Ticket
	res *big.Int
}
type DistanceSlice []*DisInfo

func (s DistanceSlice) Len() int {
	return len(s)
}

func (s DistanceSlice) Less(i, j int) bool {
	return s[i].res.Cmp(s[j].res) < 0
}

func (s DistanceSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// Finalize runs any post-transaction state modifications (e.g. block rewards)
// and assembles the final block.
// Note: The block header and state database might be updated to reflect any
// consensus rules that happen at finalization (e.g. block rewards).
func (dt *DaTong) Finalize(chain consensus.ChainReader, header *types.Header, statedb *state.StateDB, txs []*types.Transaction,
	uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
	parent, err := getParent(chain, header, glb_parents)
	if err != nil {
		return nil, err
	}
	selected := header.GetSelectedTicket()
	retreat := header.GetRetreatTickets()
	if selected == nil {
		log.Warn("Finalize shouldn't calc difficulty, as it's done in VerifyHeader or Prepare")
		if common.DebugMode {
			panic("Finalize shouldn't calc difficulty, as it's done in VerifyHeader or Prepare")
		}
		_, selected, _, retreat, err = dt.calcBlockDifficulty(chain, header, parent)
		if err != nil {
			return nil, err
		}
	}

	snap := newSnapshot()

	//update tickets
	headerState := statedb
	tickets, err := headerState.AllTickets()
	if err != nil {
		return nil, err
	}
	numTickets := tickets.NumberOfTickets()
	if numTickets <= 1 {
		log.Warn("Next block doesn't have ticket, wait buy ticket")
		return nil, errors.New("Next block doesn't have ticket, wait buy ticket")
	}

	returnTicket := func(ticket *common.Ticket) {
		if ticket.ExpireTime <= header.Time.Uint64() {
			return
		}
		value := common.NewTimeLock(&common.TimeLockItem{
			StartTime: ticket.StartTime,
			EndTime:   ticket.ExpireTime,
			Value:     ticket.Value(),
		})
		headerState.AddTimeLockBalance(ticket.Owner(), common.SystemAssetID, value, header.Number, header.Time.Uint64())
	}

	deleteTicket := func(ticket *common.Ticket, logType ticketLogType, returnBack bool) {
		id := ticket.ID
		headerState.RemoveTicket(id)
		snap.AddLog(&ticketLog{
			TicketID: id,
			Type:     logType,
		})
		if returnBack {
			returnTicket(ticket)
		}
	}

	deleteTicket(selected, ticketSelect, !selected.IsInGenesis())

	//delete tickets before coinbase if selected miner did not Seal
	for i, t := range retreat {
		if i >= maxNumberOfDeletedTickets {
			break
		}
		deleteTicket(t, ticketRetreat, !(t.IsInGenesis() || i == 0))
	}

	hash, err := headerState.UpdateTickets(header.Number, parent.Time.Uint64())
	if err != nil {
		return nil, errors.New("UpdateTickets failed: " + err.Error())
	}
	if header.MixDigest == (common.Hash{}) {
		header.MixDigest = hash
	} else if header.MixDigest != hash {
		return nil, fmt.Errorf("MixDigest mismatch, have:%v, want:%v", header.MixDigest, hash)
	}

	snap.SetTicketNumber(int(headerState.TotalNumberOfTickets()))
	snapBytes := snap.Bytes()
	header.Extra = header.Extra[:extraVanity]
	header.Extra = append(header.Extra, snapBytes...)
	header.Extra = append(header.Extra, make([]byte, extraSeal)...)
	headerState.AddBalance(header.Coinbase, common.SystemAssetID, calcRewards(header.Number))
	header.Root = headerState.IntermediateRoot(chain.Config().IsEIP158(header.Number))

	return types.NewBlock(header, txs, nil, receipts), nil
}

// Seal generates a new sealing request for the given input block and pushes
// the result into the given channel.
//
// Note, the method returns immediately and will send the result async. More
// than one result may also be returned depending on the consensus algorothm.
func (dt *DaTong) Seal(chain consensus.ChainReader, block *types.Block, results chan<- *types.Block, stop <-chan struct{}) error {
	header := block.Header()
	number := header.Number.Uint64()
	if number == 0 {
		return errUnknownBlock
	}
	dt.lock.RLock()
	signer, signFn := dt.signer, dt.signFn
	dt.lock.RUnlock()

	if signer != header.Coinbase {
		return errors.New("Mismatched Signer and Coinbase")
	}

	// delay time decide block time
	delay, errc := dt.calcDelayTime(chain, header)
	if errc != nil {
		return errc
	}

	sighash, err := signFn(accounts.Account{Address: header.Coinbase}, sigHash(header).Bytes())
	if err != nil {
		return err
	}
	copy(header.Extra[len(header.Extra)-extraSeal:], sighash)

	go func() {
		select {
		case <-stop:
			return
		case <-time.After(delay):
		}

		select {
		case results <- block.WithSeal(header):
			// One of the threads found a block, abort all others
			stop = make(chan struct{})
		default:
			log.Warn("Sealing result is not read by miner", "sealhash", dt.SealHash(header))
		}
	}()

	return nil
}

// SealHash returns the hash of a block prior to it being sealed.
func (dt *DaTong) SealHash(header *types.Header) (hash common.Hash) {
	hasher := sha3.NewKeccak256()
	rlp.Encode(hasher, []interface{}{
		header.ParentHash,
		header.UncleHash,
		header.Coinbase,
		header.Root,
		header.TxHash,
		header.ReceiptHash,
		header.Bloom,
		header.Difficulty,
		header.Number,
		header.GasLimit,
		header.GasUsed,
		header.Extra[:extraVanity],
		header.MixDigest,
		header.Nonce,
	})
	hasher.Sum(hash[:0])
	return hash
}

// CalcDifficulty is the difficulty adjustment algorithm. It returns the difficulty
// that a new block should have.
func (dt *DaTong) CalcDifficulty(chain consensus.ChainReader, time uint64, parent *types.Header) *big.Int {
	return nil
}

// APIs returns the RPC APIs this consensus engine provides.
func (dt *DaTong) APIs(chain consensus.ChainReader) []rpc.API {
	return []rpc.API{{
		Namespace: "fsn",
		Version:   "1.0",
		Service:   &API{chain: chain},
		Public:    false,
	}}
}

// Close terminates any background threads maintained by the consensus engine.
func (dt *DaTong) Close() error {
	return nil
}

func (dt *DaTong) getAllTickets(chain consensus.ChainReader, header *types.Header) (common.TicketsDataSlice, error) {
	if ts := state.GetCachedTickets(header.MixDigest); ts != nil {
		return ts, nil
	}
	statedb, err := state.New(header.Root, header.MixDigest, dt.stateCache)
	if err == nil {
		return statedb.AllTickets()
	} else if header.Number.Uint64() == 0 {
		return nil, err
	}

	// get tickets from past state
	var tickets common.TicketsDataSlice
	parent := header
	parents := []*types.Header{parent}
	for {
		if parent = chain.GetHeader(parent.ParentHash, parent.Number.Uint64()-1); parent == nil {
			return nil, fmt.Errorf("Can not find parent", "number", parent.Number.Uint64()-1, "hash", parent.ParentHash)
		}
		statedb, err = state.New(parent.Root, parent.MixDigest, dt.stateCache)
		if err == nil {
			if tickets, err = statedb.AllTickets(); err != nil {
				return nil, err
			}
			break
		} else if parent.Number.Uint64() == 0 {
			return nil, err
		}
		parents = append(parents, parent)
	}
	log.Info("getAllTickets find tickets from past state", "current", header.Number, "past", parent.Number)
	if common.DebugMode {
		defer func(bstart time.Time) {
			log.Info("getAllTickets from past state spend time", "duration", common.PrettyDuration(time.Since(bstart)))
		}(time.Now())
	}

	// deduct the current tickets
	buyTicketTopic := common.Hash{}
	buyTicketTopic[common.HashLength-1] = uint8(common.BuyTicketFunc)
	processLog := func(l *types.Log) error {
		if !(len(l.Topics) == 1 && l.Topics[0] == buyTicketTopic) {
			return nil
		}
		maps := make(map[string]interface{})
		err := json.Unmarshal(l.Data, &maps)
		if err != nil {
			return err
		}

		idstr, idok := maps["Ticket"].(string)
		datastr, dataok := maps["Base"].(string)
		if !idok || !dataok {
			return errors.New("buy ticket log has wrong data")
		}

		data, err := base64.StdEncoding.DecodeString(datastr)
		if err != nil {
			return err
		}

		buyTicketParam := common.BuyTicketParam{}
		rlp.DecodeBytes(data, &buyTicketParam)

		ticket := &common.Ticket{
			ID: common.HexToHash(idstr),
			TicketBody: common.TicketBody{
				Height:     l.BlockNumber,
				StartTime:  buyTicketParam.Start,
				ExpireTime: buyTicketParam.End,
			},
		}
		tickets, err = tickets.AddTicket(ticket)
		return err
	}
	processSnap := func(h *types.Header) error {
		snap, err := NewSnapshotFromHeader(h)
		if err != nil {
			return err
		}
		tickets, err = tickets.RemoveTicket(snap.Selected)
		if err != nil {
			return err
		}
		for _, id := range snap.Retreat {
			tickets, err = tickets.RemoveTicket(id)
			if err != nil {
				return err
			}
		}
		return nil
	}

	for i := len(parents) - 1; i >= 0; i-- {
		hash := parents[i].Hash()
		if number := rawdb.ReadHeaderNumber(dt.db, hash); number != nil {
			receipts := rawdb.ReadReceipts(dt.db, hash, *number)
			for _, receipt := range receipts {
				for _, log := range receipt.Logs {
					if err := processLog(log); err != nil {
						return nil, err
					}
				}
			}
		}
		if err := processSnap(parents[i]); err != nil {
			return nil, err
		}
	}

	tickets, err = tickets.ClearExpiredTickets(header.Time.Uint64())
	if err != nil {
		return nil, err
	}
	if err := state.AddCachedTickets(header.MixDigest, tickets); err != nil {
		return nil, err
	}
	return tickets, nil
}

func sigHash(header *types.Header) (hash common.Hash) {
	hasher := sha3.NewKeccak256()
	rlp.Encode(hasher, []interface{}{
		header.ParentHash,
		header.UncleHash,
		header.Coinbase,
		header.Root,
		header.TxHash,
		header.ReceiptHash,
		header.Bloom,
		header.Difficulty,
		header.Number,
		header.GasLimit,
		header.GasUsed,
		header.Time,
		header.Extra[:len(header.Extra)-extraSeal],
		header.MixDigest,
		header.Nonce,
	})
	hasher.Sum(hash[:0])
	return hash
}

func getSnapDataByHeader(header *types.Header) []byte {
	return getSnapData(header.Extra)
}

func getSnapData(data []byte) []byte {
	extraSuffix := len(data) - extraSeal
	return data[extraVanity:extraSuffix]
}

func calcRewards(height *big.Int) *big.Int {
	var i int64
	div2 := big.NewInt(2)
	// initial reward 2.5
	var reward = new(big.Int).Mul(big.NewInt(25), big.NewInt(100000000000000000))
	// every 4915200 blocks divide reward by 2
	segment := new(big.Int).Div(height, new(big.Int).SetUint64(4915200))
	for i = 0; i < segment.Int64(); i++ {
		reward = new(big.Int).Div(reward, div2)
	}
	return reward
}

type DisInfoWithIndex struct {
	index int
	info  *DisInfo
}

func calcDisInfo(ind int, tickets common.TicketsData, parent *types.Header, ch chan *DisInfoWithIndex) {
	parentHash := parent.Hash()
	owner := tickets.Owner

	var minTicket *common.TicketBody
	var minDist *big.Int
	for _, t := range tickets.Tickets {
		hash := parentHash
		if t.IsInGenesis() {
			index := t.StartTime
			hash = crypto.Keccak256Hash(new(big.Int).SetUint64(index).Bytes())
		}
		w := new(big.Int).Sub(parent.Number, t.BlockHeight())
		w = new(big.Int).Add(w, common.Big1)
		w2 := new(big.Int).Mul(w, w)

		tid := crypto.Keccak256Hash(owner[:], hash[:])
		id := new(big.Int).SetBytes(crypto.Keccak256(parentHash[:], tid[:], []byte(owner.Hex())))
		id2 := new(big.Int).Mul(id, id)
		s := new(big.Int).Add(w2, id2)

		if minDist == nil || s.Cmp(minDist) < 0 {
			minTicket = &t
			minDist = s
		}
	}
	ticket := &common.Ticket{
		ID:         common.TicketID(owner, minTicket.Height, minTicket.StartTime),
		TicketBody: *minTicket,
	}
	result := &DisInfoWithIndex{index: ind, info: &DisInfo{tk: ticket, res: minDist}}
	ch <- result
}

func (dt *DaTong) calcBlockDifficulty(chain consensus.ChainReader, header *types.Header, parent *types.Header) (*big.Int, *common.Ticket, uint64, common.TicketPtrSlice, error) {
	if header.GetSelectedTicket() != nil {
		return header.Difficulty, header.GetSelectedTicket(), header.Nonce.Uint64(), header.GetRetreatTickets(), nil
	}
	parentTickets, err := dt.getAllTickets(chain, parent)
	if err != nil {
		return nil, nil, 0, nil, err
	}
	haveTicket := false
	for _, v := range parentTickets {
		if v.Owner == header.Coinbase {
			haveTicket = true
			break
		}
	}
	if !haveTicket {
		return nil, nil, 0, nil, errors.New("Miner doesn't have ticket")
	}
	ticketsTotalAmount, numberOfticketOwners := parentTickets.NumberOfTicketsAndOwners()

	// calc balance before selected ticket from stored tickets list
	var (
		selected *common.Ticket
		retreat  common.TicketPtrSlice
	)

	// make consensus by tickets sequence(selectedTime) with: parentHash, weigth, ticketID, coinbase
	ch := make(chan *DisInfoWithIndex, numberOfticketOwners)
	list := make(DistanceSlice, numberOfticketOwners)
	for k, v := range parentTickets {
		go calcDisInfo(k, v, parent, ch)
	}
	for i := 0; i < int(numberOfticketOwners); i++ {
		v := <-ch
		list[v.index] = v.info
	}
	close(ch)
	sort.Sort(list)
	for _, t := range list {
		owner := t.tk.Owner()
		if owner == header.Coinbase {
			selected = t.tk
			break
		} else {
			retreat = append(retreat, t.tk) // one miner one selected ticket
		}
	}
	if selected == nil {
		return nil, nil, 0, nil, errors.New("myself tickets not selected in maxBlockTime")
	}

	selectedTime := uint64(len(retreat))

	// cacl difficulty
	difficulty := new(big.Int).SetUint64(ticketsTotalAmount - selectedTime)
	if selectedTime > 0 {
		// base10 = base * 10 (base > 1)
		base10 := int64(16)
		// exponent = max(selectedTime, 50)
		exponent := int64(selectedTime)
		if exponent > 50 {
			exponent = 50
		}
		// difficulty = ticketsTotal * pow(10, exponent) / pow(base10, exponent)
		difficulty = new(big.Int).Div(
			new(big.Int).Mul(difficulty, cmath.BigPow(10, exponent)),
			cmath.BigPow(base10, exponent))
		if difficulty.Cmp(common.Big1) < 0 {
			difficulty = common.Big1
		}
	}
	adjust := new(big.Int).SetUint64(numberOfticketOwners - selectedTime)
	difficulty = new(big.Int).Add(difficulty, adjust)

	header.SetSelectedTicket(selected)
	header.SetRetreatTickets(retreat)

	return difficulty, selected, selectedTime, retreat, nil
}

// PreProcess update state if needed from various block info
// used with some PoS Systems
func (c *DaTong) PreProcess(chain consensus.ChainReader, header *types.Header, statedb *state.StateDB) error {
	return nil
}

func (dt *DaTong) calcDelayTime(chain consensus.ChainReader, header *types.Header) (time.Duration, error) {
	list := header.Nonce.Uint64()
	if list > 0 {
		return time.Unix(header.Time.Int64(), 0).Sub(time.Now()), nil
	}

	// delayTime = ParentTime + (15 - 2) - time.Now
	parent := chain.GetHeaderByNumber(header.Number.Uint64() - 1)
	endTime := new(big.Int).Add(header.Time, new(big.Int).SetUint64(list*uint64(delayTimeModifier)+dt.config.Period-2))
	delayTime := time.Unix(endTime.Int64(), 0).Sub(time.Now())

	// delay maximum
	if (new(big.Int).Sub(endTime, header.Time)).Uint64() > maxBlockTime {
		endTime = new(big.Int).Add(header.Time, new(big.Int).SetUint64(maxBlockTime+dt.config.Period-2+list))
		delayTime = time.Unix(endTime.Int64(), 0).Sub(time.Now())
	}
	if header.Number.Uint64() > (adjustIntervalBlocks + 1) {
		// adjust = ( ( parent - gparent ) / 2 - (dt.config.Period) ) / dt.config.Period
		gparent := chain.GetHeaderByNumber(header.Number.Uint64() - 1 - adjustIntervalBlocks)
		adjust := ((time.Unix(parent.Time.Int64(), 0).Sub(time.Unix(gparent.Time.Int64(), 0)) / adjustIntervalBlocks) -
			time.Duration(int64(dt.config.Period))*time.Second) /
			time.Duration(int64(adjustIntervalBlocks))

		stampSecond := time.Duration(2) * time.Second
		if adjust > stampSecond {
			adjust = stampSecond
		} else if adjust < -stampSecond {
			adjust = -stampSecond
		}
		delayTime -= adjust
	}
	return delayTime, nil
}

// check ticket info
func (dt *DaTong) checkTicketInfo(header *types.Header, ticket *common.Ticket) error {
	// check height
	if ticket.BlockHeight().Cmp(header.Number) >= 0 {
		return errors.New("checkTicketInfo ticket height mismatch")
	}
	// check start and expire time
	if ticket.ExpireTime <= ticket.StartTime ||
		ticket.ExpireTime < (ticket.StartTime+30*24*3600) ||
		ticket.ExpireTime < header.Time.Uint64() {
		return errors.New("checkTicketInfo ticket ExpireTime mismatch")
	}
	return nil
}

// check block time
func (dt *DaTong) checkBlockTime(chain consensus.ChainReader, header *types.Header, parent *types.Header) error {
	list := header.Nonce.Uint64()
	if list <= 0 { // No.1 pass, check others
		return nil
	}
	recvTime := header.Time.Int64() - parent.Time.Int64()
	maxDelaySeconds := int64(maxBlockTime + dt.config.Period)
	if recvTime < maxDelaySeconds {
		expectTime := int64(dt.config.Period + list*delayTimeModifier)
		if recvTime < expectTime {
			return fmt.Errorf("block time mismatch: order: %v, receive: %v, expect: %v.", list, recvTime, expectTime)
		}
	}
	return nil
}

func (dt *DaTong) SetStateCache(stateCache state.Database) {
	dt.stateCache = stateCache
}
