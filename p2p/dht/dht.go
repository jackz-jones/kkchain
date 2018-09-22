package dht

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/invin/kkchain/p2p"
	log "github.com/sirupsen/logrus"
)

const (
	protocolDHT = "/kkchain/p2p/dht/1.0.0"
)

const (
	DefaultSyncTableInterval   = 20 * time.Second
	DefaultSaveTableInterval   = 1 * time.Minute
	DefaultSeedMinTableTime    = 50 * time.Second
	DefaultMaxPeersCountToSync = 6
)

type DHTConfig struct {
	BucketSize      int
	RoutingTableDir string
}

// DHT implements a Distributed Hash Table for p2p
type DHT struct {
	// self
	host    p2p.Host
	network p2p.Network

	quitCh         chan bool
	table          *RoutingTable
	store          *PeerStore
	config         *DHTConfig
	self           PeerID
	BootstrapNodes []string
	pingpong       *PingPongService
}

func DefaultConfig() *DHTConfig {
	return &DHTConfig{
		BucketSize:      BucketSize,
		RoutingTableDir: "",
	}
}

// New creates a new DHT object with the given peer as as the 'local' host
func New(config *DHTConfig, network p2p.Network, host p2p.Host) *DHT {
	// If no node database was given, use an in-memory one
	db, err := newPeerStore(config.RoutingTableDir)
	if err != nil {
		return nil
	}

	self := CreateID(host.ID().Address, host.ID().PublicKey)

	dht := &DHT{
		quitCh:   make(chan bool),
		config:   config,
		self:     self,
		table:    CreateRoutingTable(self),
		store:    db,
		pingpong: newPingPongService(),
	}

	dht.host = host
	dht.network = network

	if err := dht.host.SetMessageHandler(protocolDHT, dht.handleMessage); err != nil {
		panic(err)
	}

	// Register to get notification
	host.Register(dht)

	return dht
}

// Self returns self info
func (dht *DHT) Self() PeerID {
	return dht.self
}

// handleMessage handles messages within the stream
func (dht *DHT) handleMessage(c p2p.Conn, msg proto.Message) {
	// check message type
	switch message := msg.(type) {
	case *Message:
		go dht.doHandleMessage(c, message)
	default:
		c.Close()
		log.Errorf("unexpected message: %v", msg)
	}
}

// doHandleMessage handles messsage
func (dht *DHT) doHandleMessage(c p2p.Conn, msg *Message) {
	// get handler
	handler := dht.handlerForMsgType(msg.GetType())
	if handler == nil {
		c.Close()
		log.Errorf("unknown message type: %v", msg.GetType())
		return
	}

	// dispatch handler
	// TODO: get context and peer id
	ctx := context.Background()
	pid := c.RemotePeer()

	rpmes, err := handler(ctx, pid, msg)

	// if nil response, return it before serializing
	if rpmes == nil {
		return
	}

	// send out response msg
	if err = c.WriteMessage(rpmes, protocolDHT); err != nil {
		c.Close()
		log.Errorf("send response error: %s", err)
		return
	}

}

func (dht *DHT) Start() {
	dht.loadBootstrapNodes()

	//load table from db
	dht.loadTableFromDB()

	go dht.syncLoop()
	go dht.checkPingPong()
}

func (dht *DHT) Stop() {
	dht.quitCh <- true
}

func (dht *DHT) syncLoop() {

	dht.table.printTable()

	//first sync
	dht.SyncRouteTable()

	//TODO: config timer
	syncLoopTicker := time.NewTicker(DefaultSyncTableInterval)
	defer syncLoopTicker.Stop()

	saveTableToStore := time.NewTicker(DefaultSaveTableInterval)
	defer saveTableToStore.Stop()

	for {
		select {
		case <-dht.quitCh:
			log.Info("stopped sync loop")
			dht.store.Close()
			return
		case <-syncLoopTicker.C:
			dht.SyncRouteTable()
		case <-saveTableToStore.C:
			dht.saveTableToStore()
		}
	}
}

func (dht *DHT) AddPeer(peer PeerID) {

	//dht.store.Update(&peer)
	peer.addTime = time.Now()
	dht.table.Update(peer)
	//TODO: limit number of goroutine
	if !peer.Equals(dht.self) && dht.pingpong.GetStopCh(peer.HashHex()) == nil {
		go dht.ping(peer)
	}

}

func (dht *DHT) RemovePeer(peer PeerID) {

	dht.store.Delete(&peer)
	dht.table.RemovePeer(peer)
}

//FindTargetNeighbours searches target's neighbours from given PeerID
func (dht *DHT) FindTargetNeighbours(target []byte, peer PeerID) {
	log.WithFields(log.Fields{
		"from":   hex.EncodeToString(peer.PublicKey),
		"target": hex.EncodeToString(target),
	}).Infof("want to find a peer")

	if peer.Equals(dht.self) {
		return
	}

	conn, err := dht.host.Connection(peer.ID)
	//TODO: dial remote peer???
	if err != nil {
		conn, err = dht.host.Connect(peer.ID.Address)
		if err != nil {
			log.Error(err)
		}

		// FIXME: delay FIND_NODE to next round
		return
	}

	// TODO: send messages after handshaking
	//send find neighbours request to peer
	pmes := NewMessage(Message_FIND_NODE, hex.EncodeToString(target))
	if err = conn.WriteMessage(pmes, protocolDHT); err != nil {
		log.Error(err)
	}
}

// RandomTargetID generate random peer id for query target
func RandomTargetID() []byte {
	id := make([]byte, 32)
	rand.Read(id)

	h := sha256.New()
	h.Write(id)
	return h.Sum(nil)
}

// SyncRouteTable sync route table.
func (dht *DHT) SyncRouteTable() {
	dht.table.printTable()

	target := RandomTargetID()
	syncedPeers := make(map[string]bool)

	// sync with seed nodes.
	for _, addr := range dht.network.Bootstraps() {
		pid, err := ParsePeerAddr(addr)
		if err != nil {
			log.Errorf("connect with error: %v", err)
			continue
		}

		dht.FindTargetNeighbours(target, *pid)
		syncedPeers[hex.EncodeToString(pid.Hash)] = true
	}

	// random peer selection.
	peers := dht.table.GetPeers()
	peersCount := len(peers)
	if peersCount <= 1 {
		return
	}

	peersCountToSync := DefaultMaxPeersCountToSync
	if peersCount < peersCountToSync {
		peersCountToSync = peersCount
	}

	for i := 0; i < peersCountToSync; i++ {
		pid := peers[i]
		if syncedPeers[hex.EncodeToString(pid.Hash)] == false {
			dht.FindTargetNeighbours(target, pid)
			syncedPeers[hex.EncodeToString(pid.Hash)] = true
		}
	}
}

// saveTableToStore save peer to db
func (dht *DHT) saveTableToStore() {
	peers := dht.table.GetPeers()
	now := time.Now()
	for _, v := range peers {
		if now.Sub(v.addTime) > DefaultSeedMinTableTime {
			dht.store.Update(&v)
		}
	}
}

func (dht *DHT) loadBootstrapNodes() {
	for _, addr := range dht.network.Bootstraps() {
		peer, err := ParsePeerAddr(addr)
		if err != nil {
			continue
		}

		dht.table.Update(*peer)
	}
}

func (dht *DHT) loadTableFromDB() {
	it := dht.store.db.NewIterator(nil, nil)
	for end := false; !end; end = !it.Next() {
		peer := new(PeerID)
		err := json.Unmarshal(it.Value(), &peer)
		if err != nil {
			continue
		}
		dht.table.Update(*peer)
	}

}

// Connected is called when new connection is established
func (dht *DHT) Connected(c p2p.Conn) {
	if c.RemotePeer().Address == "" || c.RemotePeer().PublicKey == nil {
		return
	}
	id := c.RemotePeer()
	peerID := CreateID(id.Address, id.PublicKey)
	dht.AddPeer(peerID)
}

// Disconnected is called when the connection is closed
func (dht *DHT) Disconnected(c p2p.Conn) {
}

func (dht *DHT) ping(p PeerID) {

	pingTicker := time.NewTicker(10 * time.Second)
	defer pingTicker.Stop()

	peer := p.HashHex()
	stop := make(chan interface{})
	if dht.pingpong.PutStopChIfAbsent(peer, stop) != nil {
		return
	}

	for {
		select {
		case <-stop:
			dht.pingpong.DeleteStopCh(peer)
			return
		case <-pingTicker.C:
			log.WithFields(log.Fields{
				"peer": p.Address,
			}).Info("sending ping to peer")
			pmes := NewMessage(Message_PING, "")

			conn, err := dht.sendMessage(p, pmes)
			if conn != nil {
				if err == nil {
					dht.pingpong.PutPingPongAt(peer, time.Now())
				}
			}
		}
	}
}

func (dht *DHT) checkPingPong() {
	checkTicker := time.NewTicker(10 * time.Second)
	defer checkTicker.Stop()

	for {
		select {
		case <-checkTicker.C:
			for p, t := range dht.pingpong.pingpongAt {
				if time.Now().Sub(t) > 60*time.Second {
					hash, _ := hex.DecodeString(p)
					peer := PeerID{Hash: hash}
					dht.RemovePeer(peer)
					dht.pingpong.GetStopCh(p) <- new(interface{})
					dht.pingpong.DeletePingPongAt(p)
				}
			}
		}
	}
}

func (dht *DHT) sendMessage(p PeerID, msg *Message) (p2p.Conn, error) {
	conn, err := dht.host.Connection(p.ID)
	if conn == nil {
		return conn, nil
	}

	if err = conn.WriteMessage(msg, protocolDHT); err != nil {
		return conn, err
	}

	return conn, err
}
