package core

import (
	log "github.com/sirupsen/logrus"
)

// Controller managers all the submodules.
type Controller struct {
	txsCh	chan NewTxsEvent
	// txPool
	blockchain	*BlockChain		
}

// NewController creates a new controller object
func NewController(chain *BlockChain) *Controller {
	return &Controller{
		blockchain: chain,
	}
}

// Start starts the controller
func (c *Controller) Start() {
	log.Debug("controller started")
}

// Stop stops the controller
func (c *Controller) Stop() {
	log.Debug("controller stopped")
}

// Blockchain returns the underlying blockchain
func (c *Controller) Blockchain() *BlockChain {
	return c.blockchain
}
