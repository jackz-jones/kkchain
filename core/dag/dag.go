package dag

import (
	"encoding/json"
	"errors"

	"github.com/gogo/protobuf/proto"
	"github.com/invin/kkchain/core/dag/pb"
)

// Node struct
type Node struct {
	Key           interface{}
	Index         uint64
	Children      []*Node
	ParentCounter uint64
	lastState     interface{}
}

// Errors
var (
	ErrKeyNotFound       = errors.New("not found")
	ErrKeyIsExisted      = errors.New("already existed")
	ErrInvalidProtoToDag = errors.New("Protobuf message cannot be converted into Dag")
	ErrInvalidDagToProto = errors.New("Dag cannot be converted into Protobuf message")
)

// NewNode new node
func NewNode(Key interface{}, Index uint64) *Node {
	return &Node{
		Key:           Key,
		Index:         Index,
		ParentCounter: 0,
		Children:      make([]*Node, 0),
	}
}

// Index return node Index
func (n *Node) GetIndex() uint64 {
	return n.Index
}

// Index return node Index
func (n *Node) LastState() interface{} {
	return n.lastState
}

// Dag struct
type Dag struct {
	nodes  map[interface{}]*Node
	Index  uint64
	indexs map[uint64]interface{}
}

// ToProto converts domain Dag into proto Dag
func (dag *Dag) ToProto() (proto.Message, error) {

	nodes := make([]*dagpb.Node, len(dag.nodes))

	for idx, Key := range dag.indexs {
		v, ok := dag.nodes[Key]
		if !ok {
			return nil, ErrInvalidDagToProto
		}

		node := new(dagpb.Node)
		node.Index = int32(v.Index)
		//node.Key = v.Key.(string)
		node.Children = make([]int32, len(v.Children))
		for i, child := range v.Children {
			node.Children[i] = int32(child.Index)
		}

		nodes[idx] = node
	}

	return &dagpb.Dag{
		Nodes: nodes,
	}, nil
}

// FromProto converts proto Dag to domain Dag
func (dag *Dag) FromProto(msg proto.Message) error {
	if msg, ok := msg.(*dagpb.Dag); ok {
		if msg != nil {
			for _, v := range msg.Nodes {
				dag.addNodeWithIndex(uint64(v.Index), uint64(v.Index))
			}

			for _, v := range msg.Nodes {
				for _, child := range v.Children {
					dag.AddEdge(int(v.Index), int(child))
				}
			}
			return nil
		}
		return ErrInvalidProtoToDag
	}
	return ErrInvalidProtoToDag
}

// String
func (dag *Dag) String() string {
	msg, err := dag.ToProto()
	if err != nil {
		return string("")
	}
	j, _ := json.Marshal(msg)
	return string(j)
}

// NewDag new dag
func NewDag() *Dag {
	return &Dag{
		nodes:  make(map[interface{}]*Node, 0),
		Index:  0,
		indexs: make(map[uint64]interface{}, 0),
	}
}

// Len Dag len
func (dag *Dag) Len() uint64 {
	return uint64(len(dag.nodes))
}

// GetNode get node by Key
func (dag *Dag) GetNode(Key interface{}) *Node {
	if v, ok := dag.nodes[Key]; ok {
		return v
	}
	return nil
}

// GetChildrenNodes get Children nodes with Key
func (dag *Dag) GetChildrenNodes(Key interface{}) []*Node {
	if v, ok := dag.nodes[Key]; ok {
		return v.Children
	}

	return nil
}

// GetRootNodes get root nodes
func (dag *Dag) GetRootNodes() []*Node {
	nodes := make([]*Node, 0)
	for _, node := range dag.nodes {
		if node.ParentCounter == 0 {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

// GetNodes get all nodes
func (dag *Dag) GetNodes() []*Node {
	nodes := make([]*Node, 0)
	for _, node := range dag.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// AddNode add node
func (dag *Dag) AddNode(Key interface{}) error {
	if _, ok := dag.nodes[Key]; ok {
		return ErrKeyIsExisted
	}

	dag.nodes[Key] = NewNode(Key, dag.Index)
	dag.indexs[dag.Index] = Key
	dag.Index++
	return nil
}

// addNodeWithIndex add node
func (dag *Dag) addNodeWithIndex(Key interface{}, Index uint64) error {
	if _, ok := dag.nodes[Key]; ok {
		return ErrKeyIsExisted
	}

	dag.nodes[Key] = NewNode(Key, Index)
	dag.indexs[Index] = Key
	dag.Index = Index
	return nil
}

// AddEdge add edge fromKey toKey
func (dag *Dag) AddEdge(fromKey, toKey interface{}) error {
	var from, to *Node
	var ok bool

	if from, ok = dag.nodes[fromKey]; !ok {
		return ErrKeyNotFound
	}

	if to, ok = dag.nodes[toKey]; !ok {
		return ErrKeyNotFound
	}

	for _, childNode := range from.Children {
		if childNode == to {
			return ErrKeyIsExisted
		}
	}

	dag.nodes[toKey].ParentCounter++
	dag.nodes[fromKey].Children = append(from.Children, to)

	return nil
}

//IsCirclular a->b-c->a
func (dag *Dag) IsCirclular() bool {

	visited := make(map[interface{}]int, len(dag.nodes))
	rootNodes := make(map[interface{}]*Node)
	for Key, node := range dag.nodes {
		visited[Key] = 0
		rootNodes[Key] = node
	}

	for _, node := range rootNodes {
		if dag.hasCirclularDep(node, visited) {
			return true
		}
	}

	for _, count := range visited {
		if count == 0 {
			return true
		}
	}
	return false
}

func (dag *Dag) hasCirclularDep(current *Node, visited map[interface{}]int) bool {

	visited[current.Key] = 1
	for _, child := range current.Children {
		if visited[child.Key] == 1 {
			return true
		}

		if dag.hasCirclularDep(child, visited) {
			return true
		}
	}
	visited[current.Key] = 2
	return false
}
