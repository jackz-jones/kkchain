package dht

import (
	"sync"
	"time"
)

type PingPongService struct {
	mutex      *sync.RWMutex
	stopCh     map[string]chan interface{}
	pingpongAt map[string]time.Time
}

func newPingPongService() *PingPongService {
	return &PingPongService{
		mutex:      &sync.RWMutex{},
		stopCh:     make(map[string]chan interface{}),
		pingpongAt: make(map[string]time.Time),
	}
}

func (p *PingPongService) PutStopCh(key string, value chan interface{}) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.stopCh[key] = value
}

func (p *PingPongService) PutStopChIfAbsent(key string, value chan interface{}) chan interface{} {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.stopCh[key] == nil {
		p.stopCh[key] = value
		return nil
	} else {
		return p.stopCh[key]
	}
}

func (p *PingPongService) GetStopCh(key string) chan interface{} {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.stopCh[key]
}

func (p *PingPongService) DeleteStopCh(key string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	delete(p.stopCh, key)
}

func (p *PingPongService) PutPingPongAt(key string, value time.Time) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.pingpongAt[key] = value
}

func (p *PingPongService) PutPingPongAtIfAbsent(key string, value time.Time) time.Time {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	nilTime := time.Time{}
	if p.pingpongAt[key] == nilTime {
		p.pingpongAt[key] = value
		return time.Time{}
	} else {
		return p.pingpongAt[key]
	}
}

func (p *PingPongService) GetPingPongAtCh(key string) time.Time {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.pingpongAt[key]
}

func (p *PingPongService) DeletePingPongAt(key string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	delete(p.pingpongAt, key)
}
