// Copyright 2015 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package raft

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"

	"bitbucket.org/bestchai/dinv/dinvRT"
	pb "github.com/coreos/etcd/raft/raftpb"
)

//templeader is used for assert attempts
var TEMPLEADER uint64 = 555

// None is a placeholder node ID used when there is no leader.
const None uint64 = 0
const noLimit = math.MaxUint64

// Possible values for StateType.
const (
	StateFollower StateType = iota
	StateCandidate
	StateLeader
)

// StateType represents the role of a node in a cluster.
type StateType uint64

var stmap = [...]string{
	"StateFollower",
	"StateCandidate",
	"StateLeader",
}

func (st StateType) String() string {
	return stmap[uint64(st)]
}

// Config contains the parameters to start a raft.
type Config struct {
	// ID is the identity of the local raft. ID cannot be 0.
	ID uint64

	// peers contains the IDs of all nodes (including self) in the raft cluster. It
	// should only be set when starting a new raft cluster. Restarting raft from
	// previous configuration will panic if peers is set. peer is private and only
	// used for testing right now.
	peers []uint64

	// ElectionTick is the number of Node.Tick invocations that must pass between
	// elections. That is, if a follower does not receive any message from the
	// leader of current term before ElectionTick has elapsed, it will become
	// candidate and start an election. ElectionTick must be greater than
	// HeartbeatTick. We suggest ElectionTick = 10 * HeartbeatTick to avoid
	// unnecessary leader switching.
	ElectionTick int
	// HeartbeatTick is the number of Node.Tick invocations that must pass between
	// heartbeats. That is, a leader sends heartbeat messages to maintain its
	// leadership every HeartbeatTick ticks.
	HeartbeatTick int

	// Storage is the storage for raft. raft generates entries and states to be
	// stored in storage. raft reads the persisted entries and states out of
	// Storage when it needs. raft reads out the previous state and configuration
	// out of storage when restarting.
	Storage Storage
	// Applied is the last applied index. It should only be set when restarting
	// raft. raft will not return entries to the application smaller or equal to
	// Applied. If Applied is unset when restarting, raft might return previous
	// applied entries. This is a very application dependent configuration.
	Applied uint64

	// MaxSizePerMsg limits the max size of each append message. Smaller value
	// lowers the raft recovery cost(initial probing and message lost during normal
	// operation). On the other side, it might affect the throughput during normal
	// replication. Note: math.MaxUint64 for unlimited, 0 for at most one entry per
	// message.
	MaxSizePerMsg uint64
	// MaxInflightMsgs limits the max number of in-flight append messages during
	// optimistic replication phase. The application transportation layer usually
	// has its own sending buffer over TCP/UDP. Setting MaxInflightMsgs to avoid
	// overflowing that sending buffer. TODO (xiangli): feedback to application to
	// limit the proposal rate?
	MaxInflightMsgs int

	// CheckQuorum specifies if the leader should check quorum activity. Leader
	// steps down when quorum is not active for an electionTimeout.
	CheckQuorum bool

	// Logger is the logger used for raft log. For multinode which can host
	// multiple raft group, each raft group can have its own logger
	Logger Logger
}

func (c *Config) validate() error {
	if c.ID == None {
		return errors.New("cannot use none as id")
	}

	if c.HeartbeatTick <= 0 {
		return errors.New("heartbeat tick must be greater than 0")
	}

	if c.ElectionTick <= c.HeartbeatTick {
		return errors.New("election tick must be greater than heartbeat tick")
	}

	if c.Storage == nil {
		return errors.New("storage cannot be nil")
	}

	if c.MaxInflightMsgs <= 0 {
		return errors.New("max inflight messages must be greater than 0")
	}

	if c.Logger == nil {
		c.Logger = raftLogger
	}

	return nil
}

// ReadState provides state for read only query.
// It's caller's responsibility to send MsgReadIndex first before getting
// this state from ready, It's also caller's duty to differentiate if this
// state is what it requests through RequestCtx, eg. given a unique id as
// RequestCtx
type ReadState struct {
	Index      uint64
	RequestCtx []byte
}
type raft struct {
	id uint64

	Term uint64
	Vote uint64

	readState ReadState

	// the log
	raftLog *raftLog

	maxInflight int
	maxMsgSize  uint64
	prs         map[uint64]*Progress

	state StateType

	votes map[uint64]bool

	msgs []pb.Message

	// the leader id
	lead uint64
	// leadTransferee is id of the leader transfer target when its value is not zero.
	// Follow the procedure defined in raft thesis 3.10.
	leadTransferee uint64
	// New configuration is ignored if there exists unapplied configuration.
	pendingConf bool

	// number of ticks since it reached last electionTimeout when it is leader
	// or candidate.
	// number of ticks since it reached last electionTimeout or received a
	// valid message from current leader when it is a follower.
	electionElapsed int

	// number of ticks since it reached last heartbeatTimeout.
	// only leader keeps heartbeatElapsed.
	heartbeatElapsed int

	checkQuorum bool

	heartbeatTimeout int
	electionTimeout  int
	// randomizedElectionTimeout is a random number between
	// [electiontimeout, 2 * electiontimeout - 1]. It gets reset
	// when raft changes its state to follower or candidate.
	randomizedElectionTimeout int

	rand *rand.Rand
	tick func()
	step stepFunc

	logger Logger
}

func newRaft(c *Config) *raft {
	if err := c.validate(); err != nil {
		panic(err.Error())
	}
	raftlog := newLog(c.Storage, c.Logger)
	hs, cs, err := c.Storage.InitialState()
	if err != nil {
		panic(err) // TODO(bdarnell)
	}
	peers := c.peers
	if len(cs.Nodes) > 0 {
		if len(peers) > 0 {
			// TODO(bdarnell): the peers argument is always nil except in
			// tests; the argument should be removed and these tests should be
			// updated to specify their nodes through a snapshot.
			panic("cannot specify both newRaft(peers) and ConfState.Nodes)")
		}
		peers = cs.Nodes
	}
	r := &raft{
		id:               c.ID,
		lead:             None,
		readState:        ReadState{Index: None, RequestCtx: nil},
		raftLog:          raftlog,
		maxMsgSize:       c.MaxSizePerMsg,
		maxInflight:      c.MaxInflightMsgs,
		prs:              make(map[uint64]*Progress),
		electionTimeout:  c.ElectionTick,
		heartbeatTimeout: c.HeartbeatTick,
		logger:           c.Logger,
		checkQuorum:      c.CheckQuorum,
	}
	/////////////////////////////////////////////////////////////////////
	//DINV dinv
	//INITALIZATION ASSERT////////////////////////////////////////
	//dinvRT.Initalize(string((r.id%3)+65))
	//Initialize assertable variables
	dinvRT.InitDistributedAssert("", nil, "raft")
	///END DINV INIT
	/////////////////////////////////////////////////////////////
	r.rand = rand.New(rand.NewSource(int64(c.ID)))
	for _, p := range peers {
		r.prs[p] = &Progress{Next: 1, ins: newInflights(r.maxInflight)}
	}
	if !isHardStateEqual(hs, emptyState) {
		r.loadState(hs)
	}
	if c.Applied > 0 {
		raftlog.appliedTo(c.Applied)
	}
	r.becomeFollower(r.Term, None)

	var nodesStrs []string
	for _, n := range r.nodes() {
		nodesStrs = append(nodesStrs, fmt.Sprintf("%x", n))
	}
	dinvRT.Track("", "r.id,r.Term,r.Vote,r.readState,r.state,r.lead,r.leadTransferee,r.pendingConf,r.electionElapsed,r.heartbeatElapsed,r.checkQuorum,r.heartbeatTimeout,r.electionTimeout,r.randomizedElectionTimeout,r.raftLog.committed,r.raftLog.applied,r.raftLog.lastIndex,r.raftLog.lastTerm", r.id, r.Term, r.Vote, r.readState, string(r.state), r.lead, r.leadTransferee, r.pendingConf, r.electionElapsed, r.heartbeatElapsed, r.checkQuorum, r.heartbeatTimeout, r.electionTimeout, r.randomizedElectionTimeout, r.raftLog.committed, r.raftLog.applied, r.raftLog.lastIndex(), r.raftLog.lastTerm())
	r.logger.Infof("newRaft %x [peers: [%s], term: %d, commit: %d, applied: %d, lastindex: %d, lastterm: %d]", r.id, strings.Join(nodesStrs, ","), r.Term, r.raftLog.committed, r.raftLog.applied, r.raftLog.lastIndex(), r.raftLog.lastTerm())
	return r
}

func (r *raft) hasLeader() bool { return r.lead != None }

func (r *raft) softState() *SoftState { return &SoftState{Lead: r.lead, RaftState: r.state} }

func (r *raft) hardState() pb.HardState {
	return pb.HardState{
		Term:   r.Term,
		Vote:   r.Vote,
		Commit: r.raftLog.committed,
	}
}

func (r *raft) quorum() int { return len(r.prs)/2 + 1 }

func (r *raft) nodes() []uint64 {
	nodes := make([]uint64, 0, len(r.prs))
	for id := range r.prs {
		nodes = append(nodes, id)
	}
	sort.Sort(uint64Slice(nodes))
	return nodes
}

// send persists state to stable storage and then sends to its mailbox.
func (r *raft) send(m pb.Message) {
	m.From = r.id
	// do not attach term to MsgProp
	// proposals are a way to forward to the leader and
	// should be treated as local message.
	if m.Type != pb.MsgProp {
		m.Term = r.Term
	}
	r.msgs = append(r.msgs, m)
	dinvRT.Track("", "r.id,r.Term,r.Vote,r.readState,r.state,r.lead,r.leadTransferee,r.pendingConf,r.electionElapsed,r.heartbeatElapsed,r.checkQuorum,r.heartbeatTimeout,r.electionTimeout,r.randomizedElectionTimeout,r.raftLog.committed,r.raftLog.applied,r.raftLog.lastIndex,r.raftLog.lastTerm", r.id, r.Term, r.Vote, r.readState, string(r.state), r.lead, r.leadTransferee, r.pendingConf, r.electionElapsed, r.heartbeatElapsed, r.checkQuorum, r.heartbeatTimeout, r.electionTimeout, r.randomizedElectionTimeout, r.raftLog.committed, r.raftLog.applied, r.raftLog.lastIndex(), r.raftLog.lastTerm())
	//@Track
}

// sendAppend sends RPC, with entries to the given peer.
func (r *raft) sendAppend(to uint64) {
	pr := r.prs[to]
	if pr.isPaused() {
		return
	}
	m := pb.Message{}
	m.To = to

	term, errt := r.raftLog.term(pr.Next - 1)
	ents, erre := r.raftLog.entries(pr.Next, r.maxMsgSize)

	if errt != nil || erre != nil { // send snapshot if we failed to get term or entries
		if !pr.RecentActive {
			r.logger.Debugf("ignore sending snapshot to %x since it is not recently active", to)
			return
		}

		m.Type = pb.MsgSnap
		snapshot, err := r.raftLog.snapshot()
		if err != nil {
			if err == ErrSnapshotTemporarilyUnavailable {
				r.logger.Debugf("%x failed to send snapshot to %x because snapshot is temporarily unavailable", r.id, to)
				return
			}
			panic(err) // TODO(bdarnell)
		}
		if IsEmptySnap(snapshot) {
			panic("need non-empty snapshot")
		}
		m.Snapshot = snapshot
		sindex, sterm := snapshot.Metadata.Index, snapshot.Metadata.Term
		r.logger.Debugf("%x [firstindex: %d, commit: %d] sent snapshot[index: %d, term: %d] to %x [%s]",
			r.id, r.raftLog.firstIndex(), r.raftLog.committed, sindex, sterm, to, pr)
		pr.becomeSnapshot(sindex)
		r.logger.Debugf("%x paused sending replication messages to %x [%s]", r.id, to, pr)
	} else {
		m.Type = pb.MsgApp
		m.Index = pr.Next - 1
		m.LogTerm = term
		m.Entries = ents
		m.Commit = r.raftLog.committed
		if n := len(m.Entries); n != 0 {
			switch pr.State {
			// optimistically increase the next when in ProgressStateReplicate
			case ProgressStateReplicate:
				last := m.Entries[n-1].Index
				pr.optimisticUpdate(last)
				pr.ins.add(last)
			case ProgressStateProbe:
				pr.pause()
			default:
				r.logger.Panicf("%x is sending append in unhandled state %s", r.id, pr.State)
			}
		}
	}
	//@Track
	dinvRT.Track("", "r.id,r.Term,r.Vote,r.readState,r.state,r.lead,r.leadTransferee,r.pendingConf,r.electionElapsed,r.heartbeatElapsed,r.checkQuorum,r.heartbeatTimeout,r.electionTimeout,r.randomizedElectionTimeout,r.raftLog.committed,r.raftLog.applied,r.raftLog.lastIndex,r.raftLog.lastTerm", r.id, r.Term, r.Vote, r.readState, string(r.state), r.lead, r.leadTransferee, r.pendingConf, r.electionElapsed, r.heartbeatElapsed, r.checkQuorum, r.heartbeatTimeout, r.electionTimeout, r.randomizedElectionTimeout, r.raftLog.committed, r.raftLog.applied, r.raftLog.lastIndex(), r.raftLog.lastTerm())
	r.send(m)
}

// sendHeartbeat sends an empty MsgApp
func (r *raft) sendHeartbeat(to uint64) {
	// Attach the commit as min(to.matched, r.committed).
	// When the leader sends out heartbeat message,
	// the receiver(follower) might not be matched with the leader
	// or it might not have all the committed entries.
	// The leader MUST NOT forward the follower's commit to
	// an unmatched index.
	commit := min(r.prs[to].Match, r.raftLog.committed)
	m := pb.Message{
		To:     to,
		Type:   pb.MsgHeartbeat,
		Commit: commit,
	}
	//@Track
	dinvRT.Track("", "r.id,r.Term,r.Vote,r.readState,r.state,r.lead,r.leadTransferee,r.pendingConf,r.electionElapsed,r.heartbeatElapsed,r.checkQuorum,r.heartbeatTimeout,r.electionTimeout,r.randomizedElectionTimeout,r.raftLog.committed,r.raftLog.applied,r.raftLog.lastIndex,r.raftLog.lastTerm", r.id, r.Term, r.Vote, r.readState, string(r.state), r.lead, r.leadTransferee, r.pendingConf, r.electionElapsed, r.heartbeatElapsed, r.checkQuorum, r.heartbeatTimeout, r.electionTimeout, r.randomizedElectionTimeout, r.raftLog.committed, r.raftLog.applied, r.raftLog.lastIndex(), r.raftLog.lastTerm())
	r.send(m)
}

// bcastAppend sends RPC, with entries to all peers that are not up-to-date
// according to the progress recorded in r.prs.
func (r *raft) bcastAppend() {
	for id := range r.prs {
		if id == r.id {
			continue
		}
		r.sendAppend(id)
	}
	//@Track
	dinvRT.Track("", "r.id,r.Term,r.Vote,r.readState,r.state,r.lead,r.leadTransferee,r.pendingConf,r.electionElapsed,r.heartbeatElapsed,r.checkQuorum,r.heartbeatTimeout,r.electionTimeout,r.randomizedElectionTimeout,r.raftLog.committed,r.raftLog.applied,r.raftLog.lastIndex,r.raftLog.lastTerm", r.id, r.Term, r.Vote, r.readState, string(r.state), r.lead, r.leadTransferee, r.pendingConf, r.electionElapsed, r.heartbeatElapsed, r.checkQuorum, r.heartbeatTimeout, r.electionTimeout, r.randomizedElectionTimeout, r.raftLog.committed, r.raftLog.applied, r.raftLog.lastIndex(), r.raftLog.lastTerm())
}

// bcastHeartbeat sends RPC, without entries to all the peers.
func (r *raft) bcastHeartbeat() {
	for id := range r.prs {
		if id == r.id {
			continue
		}
		r.sendHeartbeat(id)
		r.prs[id].resume()
	}
	//@Track
	dinvRT.Track("", "r.id,r.Term,r.Vote,r.readState,r.state,r.lead,r.leadTransferee,r.pendingConf,r.electionElapsed,r.heartbeatElapsed,r.checkQuorum,r.heartbeatTimeout,r.electionTimeout,r.randomizedElectionTimeout,r.raftLog.committed,r.raftLog.applied,r.raftLog.lastIndex,r.raftLog.lastTerm", r.id, r.Term, r.Vote, r.readState, string(r.state), r.lead, r.leadTransferee, r.pendingConf, r.electionElapsed, r.heartbeatElapsed, r.checkQuorum, r.heartbeatTimeout, r.electionTimeout, r.randomizedElectionTimeout, r.raftLog.committed, r.raftLog.applied, r.raftLog.lastIndex(), r.raftLog.lastTerm())
}

// maybeCommit attempts to advance the commit index. Returns true if
// the commit index changed (in which case the caller should call
// r.bcastAppend).
func (r *raft) maybeCommit() bool {
	// TODO(bmizerany): optimize.. Currently naive
	mis := make(uint64Slice, 0, len(r.prs))
	for id := range r.prs {
		mis = append(mis, r.prs[id].Match)
	}
	sort.Sort(sort.Reverse(mis))
	mci := mis[r.quorum()-1]
	return r.raftLog.maybeCommit(mci, r.Term)
}

func (r *raft) reset(term uint64) {
	if r.Term != term {
		r.Term = term
		r.Vote = None
	}
	r.lead = None

	r.electionElapsed = 0
	r.heartbeatElapsed = 0
	r.resetRandomizedElectionTimeout()

	r.abortLeaderTransfer()

	r.votes = make(map[uint64]bool)
	for id := range r.prs {
		r.prs[id] = &Progress{Next: r.raftLog.lastIndex() + 1, ins: newInflights(r.maxInflight)}
		if id == r.id {
			r.prs[id].Match = r.raftLog.lastIndex()
		}
	}
	r.pendingConf = false
	//@Track
	dinvRT.Track("", "r.id,r.Term,r.Vote,r.readState,r.state,r.lead,r.leadTransferee,r.pendingConf,r.electionElapsed,r.heartbeatElapsed,r.checkQuorum,r.heartbeatTimeout,r.electionTimeout,r.randomizedElectionTimeout,r.raftLog.committed,r.raftLog.applied,r.raftLog.lastIndex,r.raftLog.lastTerm", r.id, r.Term, r.Vote, r.readState, string(r.state), r.lead, r.leadTransferee, r.pendingConf, r.electionElapsed, r.heartbeatElapsed, r.checkQuorum, r.heartbeatTimeout, r.electionTimeout, r.randomizedElectionTimeout, r.raftLog.committed, r.raftLog.applied, r.raftLog.lastIndex(), r.raftLog.lastTerm())
}

func (r *raft) appendEntry(es ...pb.Entry) {
	li := r.raftLog.lastIndex()
	for i := range es {
		es[i].Term = r.Term
		es[i].Index = li + 1 + uint64(i)
	}
	r.raftLog.append(es...)
	r.prs[r.id].maybeUpdate(r.raftLog.lastIndex())
	// Regardless of maybeCommit's return, our caller will call bcastAppend.
	r.maybeCommit()
}

// tickElection is run by followers and candidates after r.electionTimeout.
func (r *raft) tickElection() {
	r.electionElapsed++

	if r.promotable() && r.pastElectionTimeout() {
		r.electionElapsed = 0
		r.Step(pb.Message{From: r.id, Type: pb.MsgHup})
	}
	//@Track
	dinvRT.Track("", "r.id,r.Term,r.Vote,r.readState,r.state,r.lead,r.leadTransferee,r.pendingConf,r.electionElapsed,r.heartbeatElapsed,r.checkQuorum,r.heartbeatTimeout,r.electionTimeout,r.randomizedElectionTimeout,r.raftLog.committed,r.raftLog.applied,r.raftLog.lastIndex,r.raftLog.lastTerm", r.id, r.Term, r.Vote, r.readState, string(r.state), r.lead, r.leadTransferee, r.pendingConf, r.electionElapsed, r.heartbeatElapsed, r.checkQuorum, r.heartbeatTimeout, r.electionTimeout, r.randomizedElectionTimeout, r.raftLog.committed, r.raftLog.applied, r.raftLog.lastIndex(), r.raftLog.lastTerm())
}

// tickHeartbeat is run by leaders to send a MsgBeat after r.heartbeatTimeout.
func (r *raft) tickHeartbeat() {
	r.heartbeatElapsed++
	r.electionElapsed++

	if r.electionElapsed >= r.electionTimeout {
		r.electionElapsed = 0
		if r.checkQuorum {
			r.Step(pb.Message{From: r.id, Type: pb.MsgCheckQuorum})
		}
		// If current leader cannot transfer leadership in electionTimeout, it becomes leader again.
		if r.state == StateLeader && r.leadTransferee != None {
			r.abortLeaderTransfer()
		}
	}

	if r.state != StateLeader {
		return
	}

	if r.heartbeatElapsed >= r.heartbeatTimeout {
		r.heartbeatElapsed = 0
		r.Step(pb.Message{From: r.id, Type: pb.MsgBeat})
	}
	//@Track
	dinvRT.Track("", "r.id,r.Term,r.Vote,r.readState,r.state,r.lead,r.leadTransferee,r.pendingConf,r.electionElapsed,r.heartbeatElapsed,r.checkQuorum,r.heartbeatTimeout,r.electionTimeout,r.randomizedElectionTimeout,r.raftLog.committed,r.raftLog.applied,r.raftLog.lastIndex,r.raftLog.lastTerm", r.id, r.Term, r.Vote, r.readState, string(r.state), r.lead, r.leadTransferee, r.pendingConf, r.electionElapsed, r.heartbeatElapsed, r.checkQuorum, r.heartbeatTimeout, r.electionTimeout, r.randomizedElectionTimeout, r.raftLog.committed, r.raftLog.applied, r.raftLog.lastIndex(), r.raftLog.lastTerm())
}

func (r *raft) becomeFollower(term uint64, lead uint64) {
	r.step = stepFollower
	r.reset(term)
	r.tick = r.tickElection
	r.lead = lead
	r.state = StateFollower
	r.logger.Infof("%x became follower at term %d", r.id, r.Term)
}

func (r *raft) becomeCandidate() {
	// TODO(xiangli) remove the panic when the raft implementation is stable
	if r.state == StateLeader {
		panic("invalid transition [leader -> candidate]")
	}
	r.step = stepCandidate
	r.reset(r.Term + 1)
	r.tick = r.tickElection
	r.Vote = r.id
	r.state = StateCandidate
	r.logger.Infof("%x became candidate at term %d", r.id, r.Term)
	//@Track
	dinvRT.Track("", "r.id,r.Term,r.Vote,r.readState,r.state,r.lead,r.leadTransferee,r.pendingConf,r.electionElapsed,r.heartbeatElapsed,r.checkQuorum,r.heartbeatTimeout,r.electionTimeout,r.randomizedElectionTimeout,r.raftLog.committed,r.raftLog.applied,r.raftLog.lastIndex,r.raftLog.lastTerm", r.id, r.Term, r.Vote, r.readState, string(r.state), r.lead, r.leadTransferee, r.pendingConf, r.electionElapsed, r.heartbeatElapsed, r.checkQuorum, r.heartbeatTimeout, r.electionTimeout, r.randomizedElectionTimeout, r.raftLog.committed, r.raftLog.applied, r.raftLog.lastIndex(), r.raftLog.lastTerm())
}

func (r *raft) becomeLeader() {
	// TODO(xiangli) remove the panic when the raft implementation is stable
	if r.state == StateFollower {
		panic("invalid transition [follower -> leader]")
	}
	r.step = stepLeader
	r.reset(r.Term)
	r.tick = r.tickHeartbeat
	r.lead = r.id
	r.state = StateLeader
	ents, err := r.raftLog.entries(r.raftLog.committed+1, noLimit)
	if err != nil {
		r.logger.Panicf("unexpected error getting uncommitted entries (%v)", err)
	}

	for _, e := range ents {
		if e.Type != pb.EntryConfChange {
			continue
		}
		if r.pendingConf {
			panic("unexpected double uncommitted config entry")
		}
		r.pendingConf = true
	}
	r.appendEntry(pb.Entry{Data: nil})
	r.logger.Infof("%x became leader at term %d", r.id, r.Term)
	//@Track
	//dinvRT.Assert(assertLeaderMatching, getAssertLeaderMatchingValues())
	//dinvRT.Assert(assertStrongLeadership, getAssertStrongLeaderhipValues())
	dinvRT.Track("", "r.id,r.Term,r.Vote,r.readState,r.state,r.lead,r.leadTransferee,r.pendingConf,r.electionElapsed,r.heartbeatElapsed,r.checkQuorum,r.heartbeatTimeout,r.electionTimeout,r.randomizedElectionTimeout,r.raftLog.committed,r.raftLog.applied,r.raftLog.lastIndex,r.raftLog.lastTerm", r.id, r.Term, r.Vote, r.readState, string(r.state), r.lead, r.leadTransferee, r.pendingConf, r.electionElapsed, r.heartbeatElapsed, r.checkQuorum, r.heartbeatTimeout, r.electionTimeout, r.randomizedElectionTimeout, r.raftLog.committed, r.raftLog.applied, r.raftLog.lastIndex(), r.raftLog.lastTerm())
}

func (r *raft) campaign() {
	r.becomeCandidate()
	if r.quorum() == r.poll(r.id, true) {
		r.becomeLeader()
		return
	}
	for id := range r.prs {
		if id == r.id {
			continue
		}
		r.logger.Infof("%x [logterm: %d, index: %d] sent vote request to %x at term %d",
			r.id, r.raftLog.lastTerm(), r.raftLog.lastIndex(), id, r.Term)
		r.send(pb.Message{To: id, Type: pb.MsgVote, Index: r.raftLog.lastIndex(), LogTerm: r.raftLog.lastTerm()})
	}
}

func (r *raft) poll(id uint64, v bool) (granted int) {
	if v {
		r.logger.Infof("%x received vote from %x at term %d", r.id, id, r.Term)
	} else {
		r.logger.Infof("%x received vote rejection from %x at term %d", r.id, id, r.Term)
	}
	if _, ok := r.votes[id]; !ok {
		r.votes[id] = v
	}
	for _, vv := range r.votes {
		if vv {
			granted++
		}
	}
	return granted
}

var dumps = 0

func (r *raft) Step(m pb.Message) error {
	if m.Type == pb.MsgHup {
		if r.state != StateLeader {
			r.logger.Infof("%x is starting a new election at term %d", r.id, r.Term)
			r.campaign()
		} else {
			r.logger.Debugf("%x ignoring MsgHup because already leader", r.id)
		}
		return nil
	}
	if m.Type == pb.MsgTransferLeader {
		if r.state != StateLeader {
			r.logger.Debugf("%x [term %d state %v] ignoring MsgTransferLeader to %x", r.id, r.Term, r.state, m.From)
		}
	}
	/////////////////////////////////////////////////////////////////////
	//DINV dinv
	//INITALIZATION ASSERT////////////////////////////////////////
	//dinvRT.Initalize(string((r.id%3)+65))
	//Initialize assertable variables
	//fmt.Println(r.lead)
	dinvRT.AddAssertable("leader", &(r.lead), nil)
	dinvRT.AddAssertable("commited", &(r.raftLog.committed), nil)
	dinvRT.AddAssertable("applied", &(r.raftLog.applied), nil)
	dinvRT.AddAssertable("id", &(r.id), nil)
	dinvRT.AddAssertable("log", &(r.raftLog.unstable), nil)
	dinvRT.Assert(assertStrongLeadership, getAssertStrongLeaderhipValues())
	//dinvRT.Assert(assertLeaderMatching, getAssertLeaderMatchingValues())
	///END DINV INIT
	/////////////////////////////////////////////////////////////
	dinvRT.Track("", "r.id,r.Term,r.Vote,r.readState,r.state,r.lead,r.leadTransferee,r.pendingConf,r.electionElapsed,r.heartbeatElapsed,r.checkQuorum,r.heartbeatTimeout,r.electionTimeout,r.randomizedElectionTimeout,r.raftLog.committed,r.raftLog.applied,r.raftLog.lastIndex,r.raftLog.lastTerm", r.id, r.Term, r.Vote, r.readState, string(r.state), r.lead, r.leadTransferee, r.pendingConf, r.electionElapsed, r.heartbeatElapsed, r.checkQuorum, r.heartbeatTimeout, r.electionTimeout, r.randomizedElectionTimeout, r.raftLog.committed, r.raftLog.applied, r.raftLog.lastIndex(), r.raftLog.lastTerm())
	/*
		lcommit, err := r.raftLog.slice(r.raftLog.committed,r.raftLog.committed,1)
		if err !=nil {
		}
	*/

	switch {
	case m.Term == 0:
		// local message
	case m.Term > r.Term:
		lead := m.From
		if m.Type == pb.MsgVote {
			if r.checkQuorum && r.state != StateCandidate && r.electionElapsed < r.electionTimeout {
				// If a server receives a RequestVote request within the minimum election timeout
				// of hearing from a current leader, it does not update its term or grant its vote
				r.logger.Infof("%x [logterm: %d, index: %d, vote: %x] ignored vote from %x [logterm: %d, index: %d] at term %d: lease is not expired (remaining ticks: %d)",
					r.id, r.raftLog.lastTerm(), r.raftLog.lastIndex(), r.Vote, m.From, m.LogTerm, m.Index, r.Term, r.electionTimeout-r.electionElapsed)
				return nil
			}
			lead = None
		}
		r.logger.Infof("%x [term: %d] received a %s message with higher term from %x [term: %d]",
			r.id, r.Term, m.Type, m.From, m.Term)
		r.becomeFollower(m.Term, lead)
	case m.Term < r.Term:
		if r.checkQuorum && (m.Type == pb.MsgHeartbeat || m.Type == pb.MsgApp) {
			// We have received messages from a leader at a lower term. It is possible that these messages were
			// simply delayed in the network, but this could also mean that this node has advanced its term number
			// during a network partition, and it is now unable to either win an election or to rejoin the majority
			// on the old term. If checkQuorum is false, this will be handled by incrementing term numbers in response
			// to MsgVote with a higher term, but if checkQuorum is true we may not advance the term on MsgVote and
			// must generate other messages to advance the term. The net result of these two features is to minimize
			// the disruption caused by nodes that have been removed from the cluster's configuration: a removed node
			// will send MsgVotes which will be ignored, but it will not receive MsgApp or MsgHeartbeat, so it will not
			// create disruptive term increases
			r.send(pb.Message{To: m.From, Type: pb.MsgAppResp})
		} else {
			// ignore other cases
			r.logger.Infof("%x [term: %d] ignored a %s message with lower term from %x [term: %d]",
				r.id, r.Term, m.Type, m.From, m.Term)
		}
		return nil
	}
	r.step(r, m)
	return nil
}

type stepFunc func(r *raft, m pb.Message)

func stepLeader(r *raft, m pb.Message) {
	//Track
	dinvRT.Track("", "r.id,r.Term,r.Vote,r.readState,r.state,r.lead,r.leadTransferee,r.pendingConf,r.electionElapsed,r.heartbeatElapsed,r.checkQuorum,r.heartbeatTimeout,r.electionTimeout,r.randomizedElectionTimeout,r.raftLog.committed,r.raftLog.applied,r.raftLog.lastIndex,r.raftLog.lastTerm", r.id, r.Term, r.Vote, r.readState, string(r.state), r.lead, r.leadTransferee, r.pendingConf, r.electionElapsed, r.heartbeatElapsed, r.checkQuorum, r.heartbeatTimeout, r.electionTimeout, r.randomizedElectionTimeout, r.raftLog.committed, r.raftLog.applied, r.raftLog.lastIndex(), r.raftLog.lastTerm())

	// These message types do not require any progress for m.From.
	switch m.Type {
	case pb.MsgBeat:
		r.bcastHeartbeat()
		return
	case pb.MsgCheckQuorum:
		if !r.checkQuorumActive() {
			r.logger.Warningf("%x stepped down to follower since quorum is not active", r.id)
			r.becomeFollower(r.Term, None)
		}
		return
	case pb.MsgProp:
		if len(m.Entries) == 0 {
			r.logger.Panicf("%x stepped empty MsgProp", r.id)
		}
		if _, ok := r.prs[r.id]; !ok {
			// If we are not currently a member of the range (i.e. this node
			// was removed from the configuration while serving as leader),
			// drop any new proposals.
			return
		}
		if r.leadTransferee != None {
			r.logger.Debugf("%x [term %d] transfer leadership to %x is in progress; dropping proposal", r.id, r.Term, r.leadTransferee)
			return
		}

		for i, e := range m.Entries {
			if e.Type == pb.EntryConfChange {
				if r.pendingConf {
					m.Entries[i] = pb.Entry{Type: pb.EntryNormal}
				}
				r.pendingConf = true
			}
		}
		r.appendEntry(m.Entries...)
		r.bcastAppend()
		return
	case pb.MsgVote:
		r.logger.Infof("%x [logterm: %d, index: %d, vote: %x] rejected vote from %x [logterm: %d, index: %d] at term %d",
			r.id, r.raftLog.lastTerm(), r.raftLog.lastIndex(), r.Vote, m.From, m.LogTerm, m.Index, r.Term)
		r.send(pb.Message{To: m.From, Type: pb.MsgVoteResp, Reject: true})
		return
	case pb.MsgReadIndex:
		ri := None
		if r.checkQuorum {
			ri = r.raftLog.committed
		}

		r.send(pb.Message{To: m.From, Type: pb.MsgReadIndexResp, Index: ri, Entries: m.Entries})
		return
	}

	// All other message types require a progress for m.From (pr).
	pr, prOk := r.prs[m.From]
	if !prOk {
		r.logger.Debugf("%x no progress available for %x", r.id, m.From)
		return
	}
	switch m.Type {
	case pb.MsgAppResp:
		pr.RecentActive = true

		if m.Reject {
			r.logger.Debugf("%x received msgApp rejection(lastindex: %d) from %x for index %d",
				r.id, m.RejectHint, m.From, m.Index)
			if pr.maybeDecrTo(m.Index, m.RejectHint) {
				r.logger.Debugf("%x decreased progress of %x to [%s]", r.id, m.From, pr)
				if pr.State == ProgressStateReplicate {
					pr.becomeProbe()
				}
				r.sendAppend(m.From)
			}
		} else {
			oldPaused := pr.isPaused()
			if pr.maybeUpdate(m.Index) {
				switch {
				case pr.State == ProgressStateProbe:
					pr.becomeReplicate()
				case pr.State == ProgressStateSnapshot && pr.needSnapshotAbort():
					r.logger.Debugf("%x snapshot aborted, resumed sending replication messages to %x [%s]", r.id, m.From, pr)
					pr.becomeProbe()
				case pr.State == ProgressStateReplicate:
					pr.ins.freeTo(m.Index)
				}

				if r.maybeCommit() {
					r.bcastAppend()
				} else if oldPaused {
					// update() reset the wait state on this node. If we had delayed sending
					// an update before, send it now.
					r.sendAppend(m.From)
				}
				// Transfer leadership is in progress.
				if m.From == r.leadTransferee && pr.Match == r.raftLog.lastIndex() {
					r.logger.Infof("%x sent MsgTimeoutNow to %x after received MsgAppResp", r.id, m.From)
					r.sendTimeoutNow(m.From)
				}
			}
		}
	case pb.MsgHeartbeatResp:
		pr.RecentActive = true

		// free one slot for the full inflights window to allow progress.
		if pr.State == ProgressStateReplicate && pr.ins.full() {
			pr.ins.freeFirstOne()
		}
		if pr.Match < r.raftLog.lastIndex() {
			r.sendAppend(m.From)
		}
	case pb.MsgSnapStatus:
		if pr.State != ProgressStateSnapshot {
			return
		}
		if !m.Reject {
			pr.becomeProbe()
			r.logger.Debugf("%x snapshot succeeded, resumed sending replication messages to %x [%s]", r.id, m.From, pr)
		} else {
			pr.snapshotFailure()
			pr.becomeProbe()
			r.logger.Debugf("%x snapshot failed, resumed sending replication messages to %x [%s]", r.id, m.From, pr)
		}
		// If snapshot finish, wait for the msgAppResp from the remote node before sending
		// out the next msgApp.
		// If snapshot failure, wait for a heartbeat interval before next try
		pr.pause()
	case pb.MsgUnreachable:
		// During optimistic replication, if the remote becomes unreachable,
		// there is huge probability that a MsgApp is lost.
		if pr.State == ProgressStateReplicate {
			pr.becomeProbe()
		}
		r.logger.Debugf("%x failed to send message to %x because it is unreachable [%s]", r.id, m.From, pr)
	case pb.MsgTransferLeader:
		leadTransferee := m.From
		lastLeadTransferee := r.leadTransferee
		if lastLeadTransferee != None {
			if lastLeadTransferee == leadTransferee {
				r.logger.Infof("%x [term %d] transfer leadership to %x is in progress, ignores request to same node %x",
					r.id, r.Term, leadTransferee, leadTransferee)
				return
			}
			r.abortLeaderTransfer()
			r.logger.Infof("%x [term %d] abort previous transferring leadership to %x", r.id, r.Term, lastLeadTransferee)
		}
		if leadTransferee == r.id {
			r.logger.Debugf("%x is already leader. Ignored transferring leadership to self", r.id)
			return
		}
		// Transfer leadership to third party.
		r.logger.Infof("%x [term %d] starts to transfer leadership to %x", r.id, r.Term, leadTransferee)
		// Transfer leadership should be finished in one electionTimeout, so reset r.electionElapsed.
		r.electionElapsed = 0
		r.leadTransferee = leadTransferee
		if pr.Match == r.raftLog.lastIndex() {
			r.sendTimeoutNow(leadTransferee)
			r.logger.Infof("%x sends MsgTimeoutNow to %x immediately as %x already has up-to-date log", r.id, leadTransferee, leadTransferee)
		} else {
			r.sendAppend(leadTransferee)
		}
	}
}

func stepCandidate(r *raft, m pb.Message) {
	switch m.Type {
	case pb.MsgProp:
		r.logger.Infof("%x no leader at term %d; dropping proposal", r.id, r.Term)
		return
	case pb.MsgApp:
		r.becomeFollower(r.Term, m.From)
		r.handleAppendEntries(m)
	case pb.MsgHeartbeat:
		r.becomeFollower(r.Term, m.From)
		r.handleHeartbeat(m)
	case pb.MsgSnap:
		r.becomeFollower(m.Term, m.From)
		r.handleSnapshot(m)
	case pb.MsgVote:
		r.logger.Infof("%x [logterm: %d, index: %d, vote: %x] rejected vote from %x [logterm: %d, index: %d] at term %d",
			r.id, r.raftLog.lastTerm(), r.raftLog.lastIndex(), r.Vote, m.From, m.LogTerm, m.Index, r.Term)
		r.send(pb.Message{To: m.From, Type: pb.MsgVoteResp, Reject: true})
	case pb.MsgVoteResp:
		gr := r.poll(m.From, !m.Reject)
		r.logger.Infof("%x [quorum:%d] has received %d votes and %d vote rejections", r.id, r.quorum(), gr, len(r.votes)-gr)
		switch r.quorum() {
		case gr:
			r.becomeLeader()
			r.bcastAppend()
		case len(r.votes) - gr:
			r.becomeFollower(r.Term, None)
		}
	case pb.MsgTimeoutNow:
		r.logger.Debugf("%x [term %d state %v] ignored MsgTimeoutNow from %x", r.id, r.Term, r.state, m.From)
	}
}

func stepFollower(r *raft, m pb.Message) {
	switch m.Type {
	case pb.MsgProp:
		if r.lead == None {
			r.logger.Infof("%x no leader at term %d; dropping proposal", r.id, r.Term)
			return
		}
		m.To = r.lead
		r.send(m)
	case pb.MsgApp:
		r.electionElapsed = 0
		r.lead = m.From
		r.handleAppendEntries(m)
	case pb.MsgHeartbeat:
		r.electionElapsed = 0
		r.lead = m.From
		r.handleHeartbeat(m)
	case pb.MsgSnap:
		r.electionElapsed = 0
		r.handleSnapshot(m)
	case pb.MsgVote:
		if (r.Vote == None || r.Vote == m.From) && r.raftLog.isUpToDate(m.Index, m.LogTerm) {
			r.electionElapsed = 0
			r.logger.Infof("%x [logterm: %d, index: %d, vote: %x] voted for %x [logterm: %d, index: %d] at term %d",
				r.id, r.raftLog.lastTerm(), r.raftLog.lastIndex(), r.Vote, m.From, m.LogTerm, m.Index, r.Term)
			r.Vote = m.From
			r.send(pb.Message{To: m.From, Type: pb.MsgVoteResp})
		} else {
			r.logger.Infof("%x [logterm: %d, index: %d, vote: %x] rejected vote from %x [logterm: %d, index: %d] at term %d",
				r.id, r.raftLog.lastTerm(), r.raftLog.lastIndex(), r.Vote, m.From, m.LogTerm, m.Index, r.Term)
			r.send(pb.Message{To: m.From, Type: pb.MsgVoteResp, Reject: true})
		}
	case pb.MsgTimeoutNow:
		r.logger.Infof("%x [term %d] received MsgTimeoutNow from %x and starts an election to get leadership.", r.id, r.Term, m.From)
		r.campaign()
	case pb.MsgReadIndex:
		if r.lead == None {
			r.logger.Infof("%x no leader at term %d; dropping index reading msg", r.id, r.Term)
			return
		}
		m.To = r.lead
		r.send(m)
	case pb.MsgReadIndexResp:
		if len(m.Entries) != 1 {
			r.logger.Errorf("%x invalid format of MsgReadIndexResp from %x, entries count: %d", r.id, m.From, len(m.Entries))
			return
		}

		r.readState.Index = m.Index
		r.readState.RequestCtx = m.Entries[0].Data
	}
}

func (r *raft) handleAppendEntries(m pb.Message) {
	if m.Index < r.raftLog.committed {
		r.send(pb.Message{To: m.From, Type: pb.MsgAppResp, Index: r.raftLog.committed})
		return
	}

	if mlastIndex, ok := r.raftLog.maybeAppend(m.Index, m.LogTerm, m.Commit, m.Entries...); ok {
		r.send(pb.Message{To: m.From, Type: pb.MsgAppResp, Index: mlastIndex})
	} else {
		r.logger.Debugf("%x [logterm: %d, index: %d] rejected msgApp [logterm: %d, index: %d] from %x",
			r.id, r.raftLog.zeroTermOnErrCompacted(r.raftLog.term(m.Index)), m.Index, m.LogTerm, m.Index, m.From)
		r.send(pb.Message{To: m.From, Type: pb.MsgAppResp, Index: m.Index, Reject: true, RejectHint: r.raftLog.lastIndex()})
	}
}

func (r *raft) handleHeartbeat(m pb.Message) {
	r.raftLog.commitTo(m.Commit)
	r.send(pb.Message{To: m.From, Type: pb.MsgHeartbeatResp})
}

func (r *raft) handleSnapshot(m pb.Message) {
	sindex, sterm := m.Snapshot.Metadata.Index, m.Snapshot.Metadata.Term
	if r.restore(m.Snapshot) {
		r.logger.Infof("%x [commit: %d] restored snapshot [index: %d, term: %d]",
			r.id, r.raftLog.committed, sindex, sterm)
		r.send(pb.Message{To: m.From, Type: pb.MsgAppResp, Index: r.raftLog.lastIndex()})
	} else {
		r.logger.Infof("%x [commit: %d] ignored snapshot [index: %d, term: %d]",
			r.id, r.raftLog.committed, sindex, sterm)
		r.send(pb.Message{To: m.From, Type: pb.MsgAppResp, Index: r.raftLog.committed})
	}
}

// restore recovers the state machine from a snapshot. It restores the log and the
// configuration of state machine.
func (r *raft) restore(s pb.Snapshot) bool {
	if s.Metadata.Index <= r.raftLog.committed {
		return false
	}
	if r.raftLog.matchTerm(s.Metadata.Index, s.Metadata.Term) {
		r.logger.Infof("%x [commit: %d, lastindex: %d, lastterm: %d] fast-forwarded commit to snapshot [index: %d, term: %d]",
			r.id, r.raftLog.committed, r.raftLog.lastIndex(), r.raftLog.lastTerm(), s.Metadata.Index, s.Metadata.Term)
		r.raftLog.commitTo(s.Metadata.Index)
		return false
	}

	r.logger.Infof("%x [commit: %d, lastindex: %d, lastterm: %d] starts to restore snapshot [index: %d, term: %d]",
		r.id, r.raftLog.committed, r.raftLog.lastIndex(), r.raftLog.lastTerm(), s.Metadata.Index, s.Metadata.Term)

	r.raftLog.restore(s)
	r.prs = make(map[uint64]*Progress)
	for _, n := range s.Metadata.ConfState.Nodes {
		match, next := uint64(0), uint64(r.raftLog.lastIndex())+1
		if n == r.id {
			match = next - 1
		} else {
			match = 0
		}
		r.setProgress(n, match, next)
		r.logger.Infof("%x restored progress of %x [%s]", r.id, n, r.prs[n])
	}
	return true
}

// promotable indicates whether state machine can be promoted to leader,
// which is true when its own id is in progress list.
func (r *raft) promotable() bool {
	_, ok := r.prs[r.id]
	return ok
}

func (r *raft) addNode(id uint64) {
	if _, ok := r.prs[id]; ok {
		// Ignore any redundant addNode calls (which can happen because the
		// initial bootstrapping entries are applied twice).
		return
	}

	r.setProgress(id, 0, r.raftLog.lastIndex()+1)
	r.pendingConf = false
}

func (r *raft) removeNode(id uint64) {
	r.delProgress(id)
	r.pendingConf = false

	// do not try to commit or abort transferring if there is no nodes in the cluster.
	if len(r.prs) == 0 {
		return
	}

	// The quorum size is now smaller, so see if any pending entries can
	// be committed.
	if r.maybeCommit() {
		r.bcastAppend()
	}
	// If the removed node is the leadTransferee, then abort the leadership transferring.
	if r.state == StateLeader && r.leadTransferee == id {
		r.abortLeaderTransfer()
	}
}

func (r *raft) resetPendingConf() { r.pendingConf = false }

func (r *raft) setProgress(id, match, next uint64) {
	r.prs[id] = &Progress{Next: next, Match: match, ins: newInflights(r.maxInflight)}
}

func (r *raft) delProgress(id uint64) {
	delete(r.prs, id)
}

func (r *raft) loadState(state pb.HardState) {
	if state.Commit < r.raftLog.committed || state.Commit > r.raftLog.lastIndex() {
		r.logger.Panicf("%x state.commit %d is out of range [%d, %d]", r.id, state.Commit, r.raftLog.committed, r.raftLog.lastIndex())
	}
	r.raftLog.committed = state.Commit
	r.Term = state.Term
	r.Vote = state.Vote
}

// pastElectionTimeout returns true iff r.electionElapsed is greater
// than or equal to the randomized election timeout in
// [electiontimeout, 2 * electiontimeout - 1].
func (r *raft) pastElectionTimeout() bool {
	return r.electionElapsed >= r.randomizedElectionTimeout
}

func (r *raft) resetRandomizedElectionTimeout() {
	r.randomizedElectionTimeout = r.electionTimeout + r.rand.Intn(r.electionTimeout)
}

// checkQuorumActive returns true if the quorum is active from
// the view of the local raft state machine. Otherwise, it returns
// false.
// checkQuorumActive also resets all RecentActive to false.
func (r *raft) checkQuorumActive() bool {
	var act int

	for id := range r.prs {
		if id == r.id { // self is always active
			act++
			continue
		}

		if r.prs[id].RecentActive {
			act++
		}

		r.prs[id].RecentActive = false
	}

	return act >= r.quorum()
}

func (r *raft) sendTimeoutNow(to uint64) {
	r.send(pb.Message{To: to, Type: pb.MsgTimeoutNow})
}

func (r *raft) abortLeaderTransfer() {
	r.leadTransferee = None
}

///////DINV dinv DINV ASSERT FUNCTIONS //////////////////////
func assertLeaderMatching(values map[string]map[string]interface{}) bool {
	//fmt.Println("Asserting Leaders Match")
	peers := dinvRT.GetPeers()

	//true if the leaders are actually a uint
	leadersUint := false
	leaders64 := make([]int64, 0)
	leadersu64 := make([]uint64, 0)
	//fmt.Println(values)

	for _, p := range peers {
		if _, ok := values[p]["leader"]; ok {
			//The inital value of leader is 0. When marshalling this
			//via the assert library the 0 comes through as an int64.
			//When the first leader is chosen it is set to a large
			//random uint64. If I cast the 0 to uint64 I get an
			//typecast error. If I cast the 9292391232939239 to an
			//int64 I get a typecast error. I (stew) made this
			//switching function to dynamically handle this issue
			switch values[p]["leader"].(type) {
			case int64:
				leadersUint = false
				leaders64 = append(leaders64, values[p]["leader"].(int64))
			case uint64:
				leadersUint = true
				leadersu64 = append(leadersu64, values[p]["leader"].(uint64))
			}
		}
	}

	if leadersUint {
		for i := range leadersu64 {
			for j := i; j < len(leadersu64); j++ {
				if leadersu64[i] != leadersu64[j] {
					fmt.Println("ASSERTION FAILURE: LEADERS DONT MATCH")
					return false
				}
			}
		}
	} else {
		for i := range leaders64 {
			for j := i; j < len(leaders64); j++ {
				if leaders64[i] != leaders64[j] {
					fmt.Println("ASSERTION FAILURE: LEADERS DONT MATCH")
					return false
				}
			}
		}
	}
	return true
}

func assertStrongLeadership(values map[string]map[string]interface{}) bool {
	fmt.Println("Asserting Strong Leadership")
	peers := dinvRT.GetPeers()
	commited := make([]uint64, 0)
	//this check ensures that only the leader is making the assert
	//reguardless of who requested the assert.
	var leaderCommited uint64
	leader := false
	for _, p := range peers {
		//make sure the values exist
		_, ok1 := values[p]["leader"]
		_, ok2 := values[p]["id"]
		if ok1 && ok2 {
			//fmt.Println(values[p]["leader"])
			//fmt.Println(values[p]["id"])
			switch values[p]["leader"].(type) {
			case int64:
				//leader not yet know (bootstraping election)
				return true
			case uint64:
				if values[p]["leader"].(uint64) == values[p]["id"].(uint64) {
					switch values[p]["commited"].(type) {
					case int64:
						//again this is the base case, there is no
						//need to handel this I think
						return true
					case uint64:
						leaderCommited = values[p]["commited"].(uint64)
						leader = true
					}

				}
			}
		}

	}
	if leader {
		for _, p := range peers {
			if _, ok := values[p]["commited"]; ok {
				commited = append(commited, values[p]["commited"].(uint64))
			}
		}
		for i := range commited {
			if commited[i] > leaderCommited {
				return false
			}
		}
	}
	return true
}

func getAssertStrongLeaderhipValues() map[string][]string {
	peers := dinvRT.GetPeers()
	values := make(map[string][]string)
	for _, p := range peers {
		values[p] = append(values[p], "commited")
		values[p] = append(values[p], "leader")
		values[p] = append(values[p], "id")
	}
	return values
}

func getAssertLeaderMatchingValues() map[string][]string {
	peers := dinvRT.GetPeers()
	values := make(map[string][]string)
	for _, p := range peers {
		values[p] = append(values[p], "leader")
	}
	return values
}

/////END DINV ASSERTIONS //////////////////////
