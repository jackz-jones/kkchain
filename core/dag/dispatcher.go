package dag

import (
	"errors"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

//type value interface{}

// Callback func node
type Callback func(*Node, interface{}) (interface{}, error)

// Task struct
type Task struct {
	dependence int
	node       *Node
}

// Errors
var (
	ErrDagHasCirclular   = errors.New("dag hava circlular")
	ErrTimeout           = errors.New("dispatcher execute timeout")
	MaxConcurrentRoutine = 5000
)

// Dispatcher struct a message dispatcher dag.
type Dispatcher struct {
	concurrency      int
	cb               Callback
	muTask           sync.Mutex
	dag              *Dag
	elapseInMs       int64
	quitCh           chan bool
	queueCh          chan *Node
	tasks            map[interface{}]*Task
	queueCounter     int
	completedCounter int
	isFinsih         bool
	finishCH         chan bool
	context          interface{}
}

// NewDispatcher create Dag Dispatcher instance.
func NewDispatcher(dag *Dag, concurrency int, elapseInMs int64, context interface{}, cb Callback) *Dispatcher {
	dp := &Dispatcher{
		concurrency:      concurrency,
		elapseInMs:       elapseInMs,
		dag:              dag,
		cb:               cb,
		tasks:            make(map[interface{}]*Task),
		queueCounter:     0,
		quitCh:           make(chan bool, concurrency),
		queueCh:          make(chan *Node, dag.Len()),
		completedCounter: 0,
		finishCH:         make(chan bool, 1),
		isFinsih:         false,
		context:          context,
	}
	return dp
}

// Run dag dispatch goroutine.
func (dp *Dispatcher) Run() error {
	log.Debug("Starting Dag Dispatcher...")

	vertices := dp.dag.GetNodes()

	rootCounter := 0
	for _, node := range vertices {
		task := &Task{
			dependence: node.parentCounter,
			node:       node,
		}
		task.dependence = node.parentCounter
		dp.tasks[node.key] = task

		if task.dependence == 0 {
			rootCounter++
			dp.push(node)
		}
	}

	if rootCounter == 0 && len(vertices) > 0 {
		return ErrDagHasCirclular
	}

	//fix bug:setup different concurrent coroutine will generate different results
	if dp.concurrency == 0 {
		dp.concurrency = rootCounter
		if dp.concurrency > MaxConcurrentRoutine {
			dp.concurrency = MaxConcurrentRoutine
		}
	}
	fmt.Printf("设置并发执行交易的协程数量为:%d\n", dp.concurrency)

	return dp.execute()
}

// execute callback
func (dp *Dispatcher) execute() error {
	log.Debug("loop Dag Dispatcher.")

	//timerChan := time.NewTicker(time.Second).C

	if dp.dag.Len() < dp.concurrency {
		dp.concurrency = dp.dag.Len()
	}
	if dp.concurrency == 0 {
		return nil
	}

	var err error
	go func() {
		for i := 0; i < dp.concurrency; i++ {
			go func() {
				for {
					select {
					case <-dp.quitCh:
						log.Debug("Stoped Dag Dispatcher.")
						return
					case msg := <-dp.queueCh:
						lastState, err := dp.cb(msg, dp.context)

						if err != nil {
							dp.Stop()
						} else {
							isFinish, err := dp.onCompleteParentTask(msg, lastState)
							if err != nil {
								log.Debug("Stoped Dag Dispatcher.err:" + err.Error())
								dp.Stop()
							}
							if isFinish {
								dp.Stop()
							}
						}
					}
				}
			}()
		}

		if dp.elapseInMs > 0 {
			deadlineTimer := time.NewTimer(time.Duration(dp.elapseInMs) * time.Millisecond)
			<-deadlineTimer.C
			err = ErrTimeout
			dp.Stop()
		}
	}()

	<-dp.finishCH
	return err
}

// Stop stop goroutine.
func (dp *Dispatcher) Stop() {
	log.Debug("Stopping dag Dispatcher...")
	dp.muTask.Lock()
	defer dp.muTask.Unlock()
	if dp.isFinsih {
		return
	}
	dp.isFinsih = true

	for i := 0; i < dp.concurrency; i++ {
		select {
		case dp.quitCh <- true:
		default:
		}
	}
	dp.finishCH <- true
}

// push queue channel
func (dp *Dispatcher) push(vertx *Node) {
	dp.queueCounter++
	dp.queueCh <- vertx
}

// CompleteParentTask completed parent tasks
func (dp *Dispatcher) onCompleteParentTask(node *Node, lastState interface{}) (bool, error) {
	dp.muTask.Lock()
	defer dp.muTask.Unlock()

	key := node.key

	vertices := dp.dag.GetChildrenNodes(key)
	//只有一个依赖的情况，可以直接引用上个statedb，只有后面分叉的情况才需要再次copy多个statedb
	var lastStateDb interface{}
	if len(vertices) == 1 {
		lastStateDb = lastState
	}

	for _, node := range vertices {
		err := dp.updateDependenceTask(node.key, lastStateDb)
		if err != nil {
			return false, err
		}
	}

	dp.completedCounter++

	if dp.completedCounter == dp.queueCounter {
		if dp.queueCounter < dp.dag.Len() {
			return false, ErrDagHasCirclular
		}
		return true, nil
	}

	return false, nil
}

// updateDependenceTask task counter
func (dp *Dispatcher) updateDependenceTask(key interface{}, lastState interface{}) error {
	if _, ok := dp.tasks[key]; ok {
		dp.tasks[key].dependence--
		if dp.tasks[key].dependence == 0 {
			//加入lastState的传递
			dp.tasks[key].node.lastState = lastState
			dp.push(dp.tasks[key].node)
		}
		if dp.tasks[key].dependence < 0 {
			return ErrDagHasCirclular
		}
	}
	return nil
}
