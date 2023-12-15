package pallets

import (
	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/hash"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/centrifuge/go-substrate-rpc-client/v4/xxhash"
)

type Cluster struct {
	ClusterId ClusterId
	ManagerId types.AccountID
	ReserveId types.AccountID
	Props     ClusterProps
}

type ClustersNodes map[ClusterId][]NodePubKey

type ClusterProps struct {
	NodeProviderAuthContract types.AccountID
}

// Events
type (
	ClusterCreated struct {
		ClusterId ClusterId
	}

	ClusterNodeAdded struct {
		ClusterId  ClusterId
		NodePubKey NodePubKey
	}

	ClusterNodeRemoved struct {
		ClusterId  ClusterId
		NodePubKey NodePubKey
	}

	ClusterParamsSet struct {
		ClusterId ClusterId
	}

	ClusterGovParamsSet struct {
		ClusterId ClusterId
	}
)

type DdcClustersApi interface {
	GetClustersNodes(clusterId ClusterId) ([]NodePubKey, error)
}

type ddcClustersApi struct {
	substrateApi *gsrpc.SubstrateAPI

	clustersNodesKey []byte

	subs map[string]map[int]subscriber
	mu   sync.Mutex
}

func NewDdcClustersApi(substrateApi *gsrpc.SubstrateAPI) DdcClustersApi {
	clustersNodesKey := append(
		xxhash.New128([]byte("DdcClusters")).Sum(nil),
		xxhash.New128([]byte("ClustersNodes")).Sum(nil)...,
	)

	subs := make(map[string]map[int]subscriber)

	api := &ddcClustersApi{
		substrateApi:     substrateApi,
		clustersNodesKey: clustersNodesKey,
		subs:             subs,
		mu:               sync.Mutex{},
	}
}

func (api *ddcClustersApi) GetClustersNodes(clusterId ClusterId) ([]NodePubKey, error) {
	clusterIdBytes, err := codec.Encode(clusterId)
	if err != nil {
		return nil, err
	}
	hasher, err := hash.NewBlake2b128Concat(nil)
	if err != nil {
		return nil, err
	}
	if _, err := hasher.Write(clusterIdBytes); err != nil {
		return nil, err
	}

	moduleMethodPrefix1Key := append(
		api.clustersNodesKey,
		hasher.Sum(nil)...,
	)

	queryKey := types.NewStorageKey(moduleMethodPrefix1Key)
	keys, err := api.substrateApi.RPC.State.GetKeysLatest(queryKey)
	if err != nil {
		return nil, err
	}

	nodesKeys := make([]NodePubKey, len(keys))
	for i, key := range keys {
		var nodePubKey NodePubKey

		// Decode SCALE-encoded NodePubKey from the secondary key:
		// 	- 16 bytes - Blake2_128 hash,
		// 	- 1 byte - enum variant,
		// 	- 32 - node public key length (as long StoragePubKey is AccountId32 type).
		if err := codec.Decode(key[len(moduleMethodPrefix1Key)+16:len(moduleMethodPrefix1Key)+16+1+32], &nodePubKey); err != nil {
			return nil, err
		}

		nodesKeys[i] = nodePubKey
	}

	return nodesKeys, nil
}

func (api *ddcClustersApi) Subs() map[string]map[int]subscriber {
	return api.subs
}

func (api *ddcClustersApi) Mu() *sync.Mutex {
	return &api.mu
}
