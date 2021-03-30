package pigpaxos

import (
	"fmt"
	"github.com/acharapko/pbench/cfg"
	"github.com/acharapko/pbench/idservice"
	"github.com/acharapko/pbench/log"
	"github.com/acharapko/pbench/net"
	"github.com/acharapko/pbench/node"
	"math/rand"
	"sort"
	"sync"
	"time"
)

const GrayTimeoutMultiplier = 1000
const TickerDuration = 10

type BalSlot struct {
	idservice.Ballot
	slot int
}

type RelayToLeader struct {
	relayTimeInt int64
	BalSlot
}

type PeerGroup struct {
	nodes		[]idservice.ID
}

func (pg *PeerGroup) GetRandomNodeId(excludeId idservice.ID, gray map[idservice.ID]time.Time) idservice.ID {
	randId := pg.nodes[rand.Intn(len(pg.nodes))]
	_, isgray := gray[randId]
	for randId == excludeId || isgray {
		randId = pg.nodes[rand.Intn(len(pg.nodes))]
		_, isgray = gray[randId]
	}
	return randId
}

func (pg PeerGroup) String() string {
	return fmt.Sprintf("PeerGroup {nodes=%v}", pg.nodes)
}

// Replica for one PigPaxos instance
type Replica struct {
	node.Node
	*PigPaxos

	// pig metadata
	relayGroups       []*PeerGroup
	fixedRelays		  []idservice.ID
	NodeIdsToGroup    map[idservice.ID]int
	myRelayGroup      int

	// pig configurations
	numRelayGroups    int
	relaySlack        int
	cleanupMultiplier int
	stdtimeout        int
	useSmallP2b       bool

	// crash suspects
	GrayNodes         map[idservice.ID]time.Time

	// RoutedMsg for ongoing P1b. We keep it here, since it is not associated with a particular slot in the log
	p1bRelayRoutedMsg *RoutedMsg
	p1bRelayTime      int64  // we keep this for p1 timeout

	p2bRelaysMapByBalSlot     map[int]*RoutedMsg
	p2bRelaysTimeMapByBalSlot map[int]int64  // for p2 timeouts

	sync.RWMutex
	GrayLock sync.RWMutex
}

// NewReplica generates new Paxos replica
func NewReplica(id idservice.ID) *Replica {
	log.Debugf("PigPaxos Starting replica %v", id)
	r := new(Replica)
	r.Node = node.NewNode(id)
	r.PigPaxos = NewPigPaxos(r.Node)
	r.Register(net.ClientMsgWrapper{}, r.handleClientMsgWrapper)
	r.Register(P1b{}, r.handleP1b)
	r.Register([]P1b{}, r.handleP1bLeader)
	r.Register(P2b{}, r.handleP2b)
	r.Register(P2bAggregated{}, r.handleP2bAggregated)
	r.Register(P3{}, r.handleP3)
	r.Register(P1a{}, r.handleP1a)
	r.Register(P2a{}, r.handleP2a)
	r.Register(P3RecoverRequest{}, r.HandleP3RecoverRequest)
	r.Register(P3RecoverReply{}, r.HandleP3RecoverReply)
	r.Register(P2a{}, r.handleP2a)
	r.Register(RoutedMsg{}, r.handleRoutedMsg)

	r.p1bRelayTime = 0
	r.relaySlack = cfg.GetConfig().RelaySlack
	r.stdtimeout = cfg.GetConfig().StdPigTimeout
	r.useSmallP2b = cfg.GetConfig().UseSmallP2b
	r.cleanupMultiplier = 3
	r.p2bRelaysMapByBalSlot = make(map[int]*RoutedMsg)
	r.p2bRelaysTimeMapByBalSlot = make(map[int]int64)
	r.NodeIdsToGroup = make(map[idservice.ID]int)
	r.GrayNodes = make(map[idservice.ID]time.Time)

	knownIDs := make([]idservice.ID, 0, len(cfg.GetConfig().Addrs))
	for id := range cfg.GetConfig().Addrs {
		knownIDs = append(knownIDs, id)
	}

	sort.Slice(knownIDs, func(i, j int) bool {
		return knownIDs[i].Zone() < knownIDs[j].Zone() ||
			(knownIDs[i].Zone() == knownIDs[j].Zone() && knownIDs[i].Node() < knownIDs[j].Node())
	})

	log.Debugf("Known IDs : %v", knownIDs)

	if cfg.GetConfig().RegionsPeerGroups {
		r.numRelayGroups = cfg.GetConfig().Z
		r.relayGroups = make([]*PeerGroup, r.numRelayGroups)
		r.myRelayGroup = r.ID().Zone() - 1
		for _, id := range knownIDs {
			pgNum := id.Zone() - 1
			if r.relayGroups[pgNum] == nil {
				r.relayGroups[pgNum] = &PeerGroup{nodes: make([]idservice.ID, 0)}
			}
			r.relayGroups[pgNum].nodes = append(r.relayGroups[pgNum].nodes, id)
		}

		log.Infof("PigPaxos region computed PeerGroups: {%v}", r.relayGroups)
	} else {
		r.numRelayGroups = cfg.GetConfig().NumRelays
		r.relayGroups = r.peersToGroups(r.numRelayGroups, knownIDs)
		log.Infof("PigPaxos computed PeerGroups: {%v}", r.relayGroups)
	}

	r.fixedRelays = make([]idservice.ID, r.numRelayGroups)

	for i, pg := range r.relayGroups {
		for _, id := range pg.nodes {
			r.NodeIdsToGroup[id] = i
		}
		if cfg.GetConfig().FixedRelays {
			r.fixedRelays[i] = r.relayGroups[i].GetRandomNodeId(r.ID(), r.GrayNodes)
		}
	}

	log.Infof("PigPaxos region NodeIdsToPeerGroups: {%v}", r.NodeIdsToGroup)

	go r.startTicker()

	return r
}

/*~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
~                         Messaging
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~*/
// Overrides Broadcast in node
func (r *Replica) Broadcast(m interface{}) {
	log.Debugf("PigPaxos Broadcast Msg: {%v}", m)
	routedMsg := RoutedMsg {
		Hops: make([]idservice.ID, 1),
		IsForward: true,
		Progress: 0,
		Payload: m,
	}
	routedMsg.Hops[0] = r.ID()
	for i := 0; i < r.numRelayGroups; i++ {
		var relayId idservice.ID
		if cfg.GetConfig().FixedRelays {
			relayId = r.fixedRelays[i]
		} else {
			r.GrayLock.RLock()
			relayId = r.relayGroups[i].GetRandomNodeId(r.ID(), r.GrayNodes)
			r.GrayLock.RUnlock()
			log.Debugf("Generated Random Relay for RG #%d {%v}: %v",i, r.relayGroups[i], relayId)
		}
		_ = r.Send(relayId, routedMsg)
	}
}

// special broadcast for messages within the peer group
func (r *Replica) BroadcastToPeerGroup(pg *PeerGroup, originalSourceToExclude idservice.ID, m RoutedMsg) {
	log.Debugf("PigPaxos Broadcast to PeerGroup %v: {%v}", pg, m)
	for _, id := range pg.nodes {
		r.GrayLock.RLock()
		_, gray := r.GrayNodes[id]
		r.GrayLock.RUnlock()
		if id != r.ID() && id != originalSourceToExclude && !gray {
			go r.Send(id, m)
		}
	}
}

// overrides Send. Handles the errors by marking them to the gray list
func (r *Replica) Send(to idservice.ID, m interface{}) error {
	if to == r.ID() {
		log.Debugf("PigPaxos Self Send loop on Msg: {%v}", m)
		r.HandleMsg(m) // loopback for self
	} else {
		err := r.Node.Send(to, m)
		if err != nil {
			// add node to gray list
			r.GrayLock.Lock()
			log.Infof("Adding node %v to gray list", to)
			r.GrayNodes[to] = time.Now()
			r.GrayLock.Unlock()
		}
	}
	return nil
}

/*~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
~                         Routing
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~*/
func (r *Replica) handleRoutedMsg(m RoutedMsg) {
	log.Debugf("Node %v handling RoutedMsg {%v}", r.ID(), m)
	if m.IsForward {
		// handle the payload ourselves
		needToPropagate := false
		switch msg := m.Payload.(type) {
		case P1a:
			needToPropagate = r.handleP1aRelay(msg, m)
		case P2a:
			needToPropagate = r.handleP2aRelay(msg, m)
		case P3:
			log.Debugf("Node %v handling msg {%v}", r.ID(), msg)
			needToPropagate = true
			r.HandleP3(msg)
		}

		// forward propagation if needed
		if m.Progress == 0 && needToPropagate {
			// still not done going to the leaf nodes
			m.Progress += 1
			pgToBroadcast := r.relayGroups[r.myRelayGroup]
			m.Hops = append(m.Hops, r.ID())
			log.Debugf("Node %v forward propagating msg %v", r.ID(), m)
			r.BroadcastToPeerGroup(pgToBroadcast, m.GetPreviousProgressHop(), m)
		}
	} else {
		// backward propagation
		// we have different logic for back-propagating P1bs and P2bs
		switch relayPayload := m.Payload.(type) {
		case P1b:
			r.handleP1bRelay(relayPayload)
		case P2b:
			r.handleP2bRelay(relayPayload)
		}
	}
}

//*********************************************************************************************************************
// Forward Propagation: Phase-1
//*********************************************************************************************************************
func (r *Replica) handleP1a(m P1a)  {
	log.Debugf("Node %v handling msg {%v}", r.ID(), m)
	r.HandleP1a(m, m.Ballot.ID())
}

func (r *Replica) handleP1aRelay(m P1a, routedMsg RoutedMsg) bool {
	needToPropagate := false
	log.Debugf("Node %v handling p1aRelay msg {%v}", r.ID(), m)
	oldBallot := r.Ballot()
	if oldBallot < m.Ballot {
		if routedMsg.Progress == 0 {
			r.Lock()
			if r.p1bRelayTime > 0 {
				// this is a ballot we have not seen... and have not relayed before
				// so we can reply nack to any outstanding p1a relays
				log.Debugf("Short circuiting p1a relay. previous ballot=%v, new ballot=%v", oldBallot, m.Ballot)
				_ = r.Send(oldBallot.ID(), m)
			}
			r.p1bRelayTime = time.Now().UnixNano()
			r.p1bRelayRoutedMsg = &RoutedMsg{Progress: routedMsg.Progress, Hops: routedMsg.Hops, Payload: make([]P1b, 0)}
			needToPropagate = true
			r.Unlock()
		} else {
			log.Debugf("Stopping relay as we reached max depth")
		}
	} else {
		log.Debugf("Node %v received P1a with ballot %v, however, a newer or same ballot %v is known. Not relaying.", r.ID(), m.Ballot, oldBallot)
	}

	if routedMsg.Progress == 1 {
		r.HandleP1a(m, routedMsg.GetLastProgressHop())
	} else {
		r.HandleP1a(m, r.ID())
	}

	return needToPropagate
}

//*********************************************************************************************************************
// Forward Propagation: Phase-2
//*********************************************************************************************************************
func (r *Replica) handleP2a(m P2a) {
	log.Debugf("Node %v handling msg {%v}", r.ID(), m)
	r.HandleP2a(m, m.Ballot.ID())
}

func (r *Replica) handleP2aRelay(m P2a, routedMsg RoutedMsg) bool {
	log.Debugf("Node %v handling msg {%v}", r.ID(), m)
	if routedMsg.Progress == 1 {
		r.HandleP2a(m, routedMsg.GetLastProgressHop())
	} else {
		// we are not at the leaf level yet, so need to have a relay setup
		r.Lock()
		if _, ok := r.p2bRelaysMapByBalSlot[m.Slot]; !ok {
			r.newP2bRelay(m, routedMsg)
		} else {
			// we have this slot already. Check ballot. if we were collecting responses for lesser ballot,
			// we can reply nack to old sender with such lesser ballot.
			// if we were collecting responses for higher ballot reply nack to new sender
			p2b := r.p2bRelaysMapByBalSlot[m.Slot].Payload.(P2b)
			if p2b.Ballot < m.Ballot {
				nackP2b := &P2b{Ballot: m.Ballot, Slot: m.Slot, ID: make([]idservice.ID, 0)}
				_ = r.Send(p2b.Ballot.ID(), nackP2b)
				r.newP2bRelay(m, routedMsg)
			} else if p2b.Ballot > m.Ballot {
				nackP2b := &P2b{Ballot: p2b.Ballot, Slot: m.Slot, ID: make([]idservice.ID, 0)}
				_ = r.Send(p2b.Ballot.ID(), nackP2b)
			}
		}
		r.Unlock()
		// self loop
		r.HandleP2a(m, r.ID())
	}
	return true
}

func (r *Replica) newP2bRelay(m P2a, routedMsg RoutedMsg) {
	routedP2b := RoutedMsg{
		Hops: routedMsg.Hops,
		IsForward:false,
		Progress:routedMsg.Progress,
		Payload:P2b{Ballot: m.Ballot, Slot: m.Slot, ID: make([]idservice.ID, 0)},
	}

	r.p2bRelaysMapByBalSlot[m.Slot] = &routedP2b
	r.p2bRelaysTimeMapByBalSlot[m.Slot] = time.Now().UnixNano()
}


//*********************************************************************************************************************
// Forward Propagation: Phase-3
//*********************************************************************************************************************
func (r *Replica) handleP3(m P3) bool {
	log.Debugf("Node %v handling msg {%v}", r.ID(), m)
	r.HandleP3(m)
	return true
}

//*********************************************************************************************************************
// Reply Propagation: Phase-1
//*********************************************************************************************************************
func (r *Replica) handleP1bLeader(p1bs []P1b) {
	log.Debugf("Node %v received aggregated P1b {%v}", r.ID(), p1bs)
	for _, p1b := range p1bs {
		r.HandleP1b(p1b)
	}
}

func (r *Replica) handleP1b(m P1b) {
	if r.p1bRelayTime > 0 && r.p1bRelayRoutedMsg != nil{
		r.handleP1bRelay(m) // received p1b from leaf, aggregate it
	} else {
		log.Debugf("Node %v received P1b {%v}", r.ID(), m)
		r.HandleP1b(m)
	}
}

func (r *Replica) handleP1bRelay(m P1b) {
	r.Lock()
	defer r.Unlock()
	// we are just relaying this message
	log.Debugf("Node %v received P1b for relay {%v}", r.ID(), m)
	//r.p1bRelays = append(r.p1bRelays, m)
	r.p1bRelayRoutedMsg.Payload = append(r.p1bRelayRoutedMsg.Payload.([]P1b), m)
	if r.readyToRelayP1b(m.Ballot) {

		log.Debugf("Relaying p1bs {%v} to %v", r.p1bRelayRoutedMsg.Payload, m.Ballot.ID())
		// relay RoutedMsg downstream unless relaying back to root, in which case just sand []P1b to ease processing at leader
		if r.p1bRelayRoutedMsg.Progress == 0 {
			_ = r.Send(r.p1bRelayRoutedMsg.GetLastProgressHop(), r.p1bRelayRoutedMsg.Payload)
		} else {
			_ = r.Send(r.p1bRelayRoutedMsg.GetLastProgressHop(), r.p1bRelayRoutedMsg)
		}
		r.p1bRelayTime = 0
		r.p1bRelayRoutedMsg = nil
	}
}

func (r *Replica) readyToRelayP1b(ballot idservice.Ballot) bool {
	relayGroup := r.relayGroups[r.myRelayGroup]
	p1bs := r.p1bRelayRoutedMsg.Payload.([]P1b)
	log.Debugf("Now have %d messages to relay for p1b Ballot %v. ", len(p1bs), ballot)
	if len(p1bs) == len(relayGroup.nodes){
		return true
	}
	if len(p1bs) == len(relayGroup.nodes) - 1 {
		for _, id := range relayGroup.nodes {
			if id == ballot.ID() {
				return true
			}
		}
	}
	return false
}

//*********************************************************************************************************************
// Reply Propagation: Phase-2
//*********************************************************************************************************************
func (r *Replica) handleP2b(m P2b) {
	if r.IsLeader() {
		// we received p2b aggregated reply, so just handle it at the pigpaxos level
		r.HandleP2b(m.Slot, m.Ballot, m.ID)
	} else {
		// here we handle the P2b coming from the leaf node
		// or the rare case of P2b coming to node who was a leader but not anymore.
		// in the latter case, we are ok to just ignore
		r.handleP2bRelay(m)
	}
}

func (r *Replica) handleP2bAggregated(m P2bAggregated) {
	log.Debugf("Handling P2bAggregated: %v", m)
	if r.IsLeader() {
		r.UpdateLastExecuteByNode(m.RelayID, m.RelayLastExecute)
		// we received p2b aggregated reply, so just handle it at the pigpaxos level
		if m.MissingIDs != nil && len(m.MissingIDs) > 0 {
			ids := make([]idservice.ID, len(r.relayGroups[r.NodeIdsToGroup[m.RelayID]].nodes))
			copy(ids, r.relayGroups[r.NodeIdsToGroup[m.RelayID]].nodes)
			for _, missingId := range m.MissingIDs {
				for i, id := range ids {
					if id == missingId {
						ids[i] = ids[len(ids) - 1]
						ids[len(ids) - 1] = 0
						ids = ids[:len(ids) - 1]
						break
					}
				}
			}
			log.Debugf("Calling HandleP2b with ids: %v", ids)
			r.HandleP2b(m.Slot, m.Ballot, ids)
		} else {
			log.Debugf("Calling HandleP2b with ids: %v", r.relayGroups[r.NodeIdsToGroup[m.RelayID]].nodes)
			r.HandleP2b(m.Slot, m.Ballot, r.relayGroups[r.NodeIdsToGroup[m.RelayID]].nodes)
		}
	} else {
		log.Errorf("Can process this type of messages only on leader node: %v", m)
	}
}

func (r *Replica) handleP2bRelay(m P2b) {
	// we are just relaying this message
	r.RLock()
	p2bForRelay, haveSlot := r.p2bRelaysMapByBalSlot[m.Slot]
	r.RUnlock()
	log.Debugf("Node %v received P2b for relay {%v}", r.ID(), m)
	if !haveSlot {
		log.Debugf("Unknown P2b {%v} Ballot to relay. It may have already been replied", m)
	} else {
		p2b := p2bForRelay.Payload.(P2b)
		if p2b.Ballot == m.Ballot {
			p2b.ID = append(p2b.ID, m.ID...)
			r.Lock()
			p2bForRelay.Payload = p2b
			r.Unlock()
			log.Debugf("Now have %d messages to relay for p2b Slot %d Ballot %v", len(p2b.ID), m.Slot, m.Ballot)
			if r.readyToRelayP2b(m.Slot) {
				var missingIds []idservice.ID
				if r.relaySlack > 0 {
					missingIds = r.computeMissingIDsForP2b(p2b)
				} else {
					missingIds = make([]idservice.ID, 0)
				}
				log.Debugf("Relaying p2bs {%v} to %v", p2bForRelay, m.Ballot.ID())
				// relay RoutedMsg downstream unless relaying back to root
				if p2bForRelay.Progress == 0 {
					if r.useSmallP2b {
						p2bSmall := P2bAggregated{
							Ballot: m.Ballot,
							Slot: m.Slot,
							RelayLastExecute: r.execute - 1,
							MissingIDs: missingIds,
							RelayID: r.ID()}
						_ = r.Send(p2bForRelay.GetLastProgressHop(), p2bSmall)
					} else {
						_ = r.Send(p2bForRelay.GetLastProgressHop(), p2bForRelay.Payload)
					}
				} else {
					_ = r.Send(p2bForRelay.GetLastProgressHop(), p2bForRelay)
				}
				r.Lock()
				delete(r.p2bRelaysMapByBalSlot, m.Slot)
				delete(r.p2bRelaysTimeMapByBalSlot, m.Slot)
				r.Unlock()
			}
		} else {
			// this is normal, when follower has already received newer ballot and replies with it to let
			// this node know of higher ballot
			log.Errorf("Node %v received P2b to reply with non-matching ballot (%v) to the relay ballot (%v)",
				r.ID(), m.Ballot, p2b.Ballot)
			nackP2b := &P2b{Ballot: m.Ballot, Slot: m.Slot, ID: make([]idservice.ID, 0)}
			_ = r.Send(p2b.Ballot.ID(), nackP2b)
		}
	}
}

func (r *Replica) computeMissingIDsForP2b(p2b P2b) []idservice.ID {
	//take a copy of this group
	missingIds := make([]idservice.ID, len(r.relayGroups[r.myRelayGroup].nodes))
	copy(missingIds, r.relayGroups[r.myRelayGroup].nodes)
	for _, id := range p2b.ID {
		for i, missingId := range missingIds {
			if id == missingId {
				missingIds[i] = missingIds[len(missingIds) - 1]
				missingIds[len(missingIds) - 1] = 0
				missingIds = missingIds[:len(missingIds) - 1]
				break
			}
		}
	}
	return missingIds
}

func (r *Replica) readyToRelayP2b(m int) bool {
	r.RLock()
	defer r.RUnlock()
	if r.p2bRelaysMapByBalSlot[m] == nil {
		return false
	}
	pgToRelay := r.relayGroups[r.myRelayGroup]
	p2b := r.p2bRelaysMapByBalSlot[m].Payload.(P2b)
	if len(p2b.ID) == len(pgToRelay.nodes) - r.relaySlack  {
		return true
	}
	if len(p2b.ID) == len(pgToRelay.nodes) - 1 - r.relaySlack {
		if r.NodeIdsToGroup[p2b.Ballot.ID()] == r.myRelayGroup {
			return true
		}
	}
	return false
}

//*********************************************************************************************************************
// Client Request Handling
//*********************************************************************************************************************
func (r *Replica) handleClientMsgWrapper(cmw net.ClientMsgWrapper) {
	log.Debugf("Replica %s received %v\n", r.ID(), cmw)
	switch cmw.Msg.(type) {
	case net.Request:
		m := cmw.Msg.(net.Request)
		if !cfg.GetConfig().EphemeralLeader || r.PigPaxos.IsLeader() || r.PigPaxos.Ballot() == 0 {
			r.PigPaxos.HandleRequest(m, cmw.Reply)
		} else {
			go r.Forward(r.PigPaxos.Leader(), m)
		}
	default:
		log.Errorf("unknown message type received form client on node %v", r.ID())
	}
}


/*~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
~                         Helpers
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~*/
func (r *Replica) peersToGroups(numGroups int, nodeList []idservice.ID) []*PeerGroup {
	peerGroups := make([]*PeerGroup, numGroups)
	pgNum := 0
	nodesAddToPg := 0
	nodesPerGroup := len(nodeList) / numGroups
	for _, id := range nodeList {
		if id == r.ID() {
			r.myRelayGroup = pgNum
		}
		if peerGroups[pgNum] == nil {
			peerGroups[pgNum] = &PeerGroup{nodes:make([]idservice.ID, 0)}
		}

		peerGroups[pgNum].nodes = append(peerGroups[pgNum].nodes, id)
		nodesAddToPg++
		if nodesAddToPg >= nodesPerGroup && pgNum+1 < numGroups {
			pgNum++
			nodesAddToPg = 0
		}
	}
	return peerGroups
}

//*********************************************************************************************************************
// Timer for all timed events, such as timeouts and log clean ups
//*********************************************************************************************************************
func (r *Replica) startTicker() {
	var ticks uint64 = 0
	for now := range time.Tick(TickerDuration * time.Millisecond) {
		// log cleanup
		ticks++
		if ticks % uint64(r.cleanupMultiplier) == 0 {
			r.CleanupLog()
		}

		if ticks % uint64(GrayTimeoutMultiplier) == 0 {
			log.Debugf("Ticker gray check on tick %d", ticks)
			r.GrayLock.Lock()
			for grayId, t := range r.GrayNodes {
				if t.Add(time.Duration(TickerDuration*GrayTimeoutMultiplier) * time.Millisecond).Before(now) {
					log.Infof("Removing node %v from gray list on timeout", grayId)
					delete(r.GrayNodes, grayId)
				}
			}
			r.GrayLock.Unlock()
			log.Debugf("Ticker gray check done on tick %d", ticks)
		}

		// handling timeouts
		timeoutCutoffTime := now.Add(-time.Duration(r.stdtimeout) * time.Millisecond).UnixNano() // everything older than this needs to timeout
		//log.Debugf("Start TimeoutChecker (timeout_cutoff = %d)", timeoutCutoffTime)
		if r.IsLeader() {
			r.CheckTimeout(timeoutCutoffTime)
		} else {
			// check for P1b timeouts
			r.Lock()
			if r.p1bRelayRoutedMsg != nil {
				p1bs := r.p1bRelayRoutedMsg.Payload.([]P1b)
				if r.p1bRelayTime > 0 && r.p1bRelayTime < timeoutCutoffTime && len(p1bs) > 0 {
					// we have timeout on P1b
					log.Debugf("Timeout on P1b. Relaying p1bs {%v}", r.p1bRelayRoutedMsg.Payload)

					if r.p1bRelayRoutedMsg.Progress == 0 {
						_ = r.Send(r.p1bRelayRoutedMsg.GetLastProgressHop(), p1bs)
					} else {
						_ = r.Send(r.p1bRelayRoutedMsg.GetLastProgressHop(), r.p1bRelayRoutedMsg)
					}
					r.p1bRelayRoutedMsg = nil
					r.p1bRelayTime = 0
				}
			}
			// check for p2b timeouts
			for slot, routedP2b := range r.p2bRelaysMapByBalSlot {
				if r.p2bRelaysTimeMapByBalSlot[slot] < timeoutCutoffTime {
					routedP2b.IsForward = false
					log.Debugf("Timeout on P2b. Relaying p2bs {%v}", r.p2bRelaysMapByBalSlot[slot])
					if routedP2b.Progress == 0 {
						p2b := routedP2b.Payload.(P2b)
						if r.useSmallP2b {
							p2bSmall := P2bAggregated{
								Ballot: p2b.Ballot,
								Slot: p2b.Slot,
								RelayLastExecute: r.execute - 1,
								RelayID: r.ID(),
								MissingIDs: r.computeMissingIDsForP2b(p2b),
							}
							_ = r.Send(routedP2b.GetLastProgressHop(), p2bSmall)
						} else {
							_ = r.Send(routedP2b.GetLastProgressHop(), routedP2b.Payload)
						}
					} else {
						_ = r.Send(routedP2b.GetLastProgressHop(), routedP2b)
					}
					delete(r.p2bRelaysMapByBalSlot, slot)
					delete(r.p2bRelaysTimeMapByBalSlot, slot)
				}
			}
			r.Unlock()
		}
	}
}
