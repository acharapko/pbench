package pigpaxos

import (
	"encoding/gob"
	"fmt"
	"github.com/acharapko/pbench/db"
	"github.com/acharapko/pbench/idservice"
)

func init() {
	gob.Register(P1b{})
	gob.Register(P2b{})
	gob.Register(P2bAggregated{})
	gob.Register([]P1b{})
	gob.Register([]P2b{})
	gob.Register(P1a{})
	gob.Register(P2a{})
	gob.Register(P3{})
	gob.Register(P3RecoverRequest{})
	gob.Register(P3RecoverReply{})
	gob.Register(RoutedMsg{})
}

// CommandBallot combines each command with its ballot number
type CommandBallot struct {
	Command db.Command
	Ballot  idservice.Ballot
}

func (cb CommandBallot) String() string {
	return fmt.Sprintf("cmd=%v b=%v", cb.Command, cb.Ballot)
}

type RoutedMsg struct {
	Hops 			[]idservice.ID
	IsForward	 	bool
	Progress 		uint8
	Payload         interface{}
}

func (m *RoutedMsg) GetLastProgressHop() idservice.ID {
	return m.Hops[m.Progress]
}

func (m *RoutedMsg) GetPreviousProgressHop() idservice.ID {
	return m.Hops[m.Progress - 1]
}

func (m RoutedMsg) String() string {
	return fmt.Sprintf("RoutedMsg {Hops=%v IsForward=%v Progress=%v, Payload=%v}",  m.Hops, m.IsForward, m.Progress, m.Payload)
}

// P1b promise message
type P1b struct {
	ID     idservice.ID               // from node id
	Ballot idservice.Ballot
	Log    map[int]CommandBallot // uncommitted logs
}

func (m P1b) String() string {
	return fmt.Sprintf("P1b {b=%v id=%s log=%v}",  m.Ballot, m.ID, m.Log)
}

// P2b accepted message
type P2b struct {
	ID      		[]idservice.ID // from node id
	Ballot  		idservice.Ballot
	Slot    		int
}

func (m P2b) String() string {
	return fmt.Sprintf("P2b {b=%v id=%s s=%d}",  m.Ballot, m.ID, m.Slot)
}

// P2b accepted message
type P2bAggregated struct {
	MissingIDs       []idservice.ID // node ids not collected by relay
	RelayID			 idservice.ID
	RelayLastExecute int
	Ballot  		 idservice.Ballot
	Slot    		 int
}

func (m P2bAggregated) String() string {
	return fmt.Sprintf("P2b {b=%v RelayId=%s RelayLastExecute=%d s=%d, missingIDs=%v}",  m.Ballot, m.RelayID, m.RelayLastExecute, m.Slot, m.MissingIDs)
}

// P1a prepare message
type P1a struct {
	Ballot idservice.Ballot
}

func (m P1a) String() string {
	return fmt.Sprintf("P1a {b=%v}", m.Ballot)
}

// P2a accept message
type P2a struct {
	Ballot  		idservice.Ballot
	Slot    		int
	GlobalExecute   int
	Command 		db.Command
	P3msg			P3
}

func (m P2a) String() string {
	return fmt.Sprintf("P2a {b=%v s=%d cmd=%v, p3Msg=%v}", m.Ballot, m.Slot, m.Command, m.P3msg)
}


// P3 commit message
type P3 struct {
	Ballot  idservice.Ballot
	Slot    []int
	//Command paxi.Command
}

func (m P3) String() string {
	return fmt.Sprintf("P3 {b=%v slots=%d}",  m.Ballot, m.Slot)
}

type P3RecoverRequest struct {
	Ballot idservice.Ballot
	Slot   int
	NodeId idservice.ID
}

func (m P3RecoverRequest) String() string {
	return fmt.Sprintf("P3RecoverRequest {b=%v slots=%d, nodeToRecover=%v}",  m.Ballot, m.Slot, m.NodeId)
}

type P3RecoverReply struct {
	Ballot  idservice.Ballot
	Slot    int
	Command db.Command
}

func (m P3RecoverReply) String() string {
	return fmt.Sprintf("P3RecoverReply {b=%v slots=%d, cmd=%v}",  m.Ballot, m.Slot, m.Command)
}