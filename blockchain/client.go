package blockchain

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/cerebellum-network/cere-ddc-sdk-go/blockchain/pallets"
)

type EventsListener func(events *pallets.Events, blockNumber types.BlockNumber, blockHash types.Hash)

type Client struct {
	*gsrpc.SubstrateAPI

	eventsListeners map[int]EventsListener
	mu              sync.Mutex
	isListening     uint32
	cancelListening func()
	errsListening   chan error

	DdcClusters  pallets.DdcClustersApi
	DdcCustomers pallets.DdcCustomersApi
	DdcNodes     pallets.DdcNodesApi
	DdcPayouts   pallets.DdcPayoutsApi
}

func NewClient(url string) (*Client, error) {
	substrateApi, err := gsrpc.NewSubstrateAPI(url)
	if err != nil {
		return nil, err
	}
	meta, err := substrateApi.RPC.State.GetMetadataLatest()
	if err != nil {
		return nil, err
	}

	return &Client{
		SubstrateAPI:    substrateApi,
		eventsListeners: make(map[int]EventsListener),
		DdcClusters:     pallets.NewDdcClustersApi(substrateApi),
		DdcCustomers:    pallets.NewDdcCustomersApi(substrateApi, meta),
		DdcNodes:        pallets.NewDdcNodesApi(substrateApi, meta),
		DdcPayouts:      pallets.NewDdcPayoutsApi(substrateApi, meta),
	}, nil
}

func (c *Client) StartEventsListening() (context.CancelFunc, <-chan error, error) {
	if !atomic.CompareAndSwapUint32(&c.isListening, 0, 1) {
		return c.cancelListening, c.errsListening, nil
	}

	meta, err := c.RPC.State.GetMetadataLatest()
	if err != nil {
		return nil, nil, err
	}
	key, err := types.CreateStorageKey(meta, "System", "Events", nil)
	if err != nil {
		return nil, nil, err
	}
	sub, err := c.RPC.State.SubscribeStorageRaw([]types.StorageKey{key})
	if err != nil {
		return nil, nil, err
	}

	done := make(chan struct{})
	c.errsListening = make(chan error)

	go func() {
		for {
			select {
			case <-done:
				return
			case set := <-sub.Chan():
				c.processSystemEventsStorageChanges(
					set.Changes,
					meta,
					key,
					set.Block,
				)
			}
		}
	}()

	once := sync.Once{}
	c.cancelListening = func() {
		once.Do(func() {
			done <- struct{}{}
			sub.Unsubscribe()
			c.isListening = 0
		})
	}

	return c.cancelListening, c.errsListening, nil
}

// RegisterEventsListener subscribes given callback to blockchain events. There is a begin parameter which
// can be used to get events from blocks older than the latest block. If begin is greater than the latest
// block number, the listener will start from the latest block.
func (c *Client) RegisterEventsListener(begin types.BlockNumber, callback EventsListener) (context.CancelFunc, error) {
	var idx int
	for i := 0; i <= math.MaxInt; i++ {
		if _, ok := c.eventsListeners[i]; !ok {
			idx = i
			break
		}
		if i == math.MaxInt {
			return nil, fmt.Errorf("too many events listeners")
		}
	}

	// Collect events starting from the latest block to process them after completion with old blocks.
	pendingEvents := &pendingEvents{}
	subscriptionStartBlock := uint32(0)
	subscriptionStarted := make(chan struct{})
	callbackWrapper := func(events *pallets.Events, blockNumber types.BlockNumber, blockHash types.Hash) {
		if atomic.CompareAndSwapUint32(&subscriptionStartBlock, 0, uint32(blockNumber)) {
			close(subscriptionStarted)
		}

		if pendingEvents.TryPush(events, blockHash, blockNumber) {
			return
		}

		callback(events, blockNumber, blockHash)
	}

	c.mu.Lock()
	c.eventsListeners[idx] = callbackWrapper
	c.mu.Unlock()

	cancelled := false

	go func() {
		<-subscriptionStarted

		if begin >= types.BlockNumber(subscriptionStartBlock) {
			return
		}

		// TODO: get for begin block and update each runtime upgrade
		meta, err := c.RPC.State.GetMetadataLatest()
		if err != nil {
			c.errsListening <- fmt.Errorf("get metadata: %w", err)
			return
		}

		key, err := types.CreateStorageKey(meta, "System", "Events")
		if err != nil {
			c.errsListening <- fmt.Errorf("create storage key: %w", err)
			return
		}

		for currentBlock := uint32(begin); currentBlock < subscriptionStartBlock; currentBlock++ {
			bHash, err := c.RPC.Chain.GetBlockHash(uint64(currentBlock))
			if err != nil {
				c.errsListening <- fmt.Errorf("get block hash: %w", err)
				return
			}

			blockChangesSets, err := c.RPC.State.QueryStorageAt([]types.StorageKey{key}, bHash)
			if err != nil {
				c.errsListening <- fmt.Errorf("query storage: %w", err)
				return
			}

			for _, set := range blockChangesSets {
				header, err := c.RPC.Chain.GetHeader(set.Block)
				if err != nil {
					c.errsListening <- fmt.Errorf("get header: %w", err)
					return
				}

				for _, change := range set.Changes {
					if !codec.Eq(change.StorageKey, key) || !change.HasStorageData {
						continue
					}

					events := &pallets.Events{}
					err = types.EventRecordsRaw(change.StorageData).DecodeEventRecords(meta, events)
					if err != nil {
						c.errsListening <- fmt.Errorf("events decoder: %w", err)
						continue
					}

					if cancelled {
						return
					}

					callback(events, header.Number, set.Block)
				}
			}
		}

		pendingEvents.Do(callback)
	}()

	once := sync.Once{}
	cancel := func() {
		once.Do(func() {
			c.mu.Lock()
			cancelled = true
			delete(c.eventsListeners, idx)
			c.mu.Unlock()
		})
	}

	return cancel, nil
}

func (c *Client) processSystemEventsStorageChanges(
	changes []types.KeyValueOption,
	meta *types.Metadata,
	storageKey types.StorageKey,
	blockHash types.Hash,
) {
	header, err := c.RPC.Chain.GetHeader(blockHash)
	if err != nil {
		c.errsListening <- fmt.Errorf("get header: %w", err)
		return
	}

	for _, change := range changes {
		if !codec.Eq(change.StorageKey, storageKey) || !change.HasStorageData {
			continue
		}

		events := &pallets.Events{}
		err = types.EventRecordsRaw(change.StorageData).DecodeEventRecords(meta, events)
		if err != nil {
			c.errsListening <- fmt.Errorf("events decoder: %w", err)
			continue
		}

		c.mu.Lock()
		for _, callback := range c.eventsListeners {
			go callback(events, header.Number, blockHash)
		}
		c.mu.Unlock()
	}
}

type blockEvents struct {
	Events *pallets.Events
	Hash   types.Hash
	Number types.BlockNumber
}

type pendingEvents struct {
	list []*blockEvents
	mu   sync.Mutex
	done bool
}

func (pe *pendingEvents) TryPush(events *pallets.Events, hash types.Hash, number types.BlockNumber) bool {
	pe.mu.Lock()
	if !pe.done {
		pe.list = append(pe.list, &blockEvents{
			Events: events,
			Hash:   hash,
			Number: number,
		})
		pe.mu.Unlock()
		return true
	}
	pe.mu.Unlock()
	return false
}

func (pe *pendingEvents) Do(callback EventsListener) {
	for {
		pe.mu.Lock()

		if len(pe.list) == 0 {
			pe.done = true
			pe.mu.Unlock()
			break
		}

		callback(pe.list[0].Events, pe.list[0].Number, pe.list[0].Hash)

		pe.list = pe.list[1:]
		pe.mu.Unlock()
	}
}
