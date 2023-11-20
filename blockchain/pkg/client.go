package pkg

import (
	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"

	"github.com/cerebellum-network/cere-ddc-sdk-go/blockchain/pkg/pallets"
)

type Client struct {
	*gsrpc.SubstrateAPI

	DdcClusters  *pallets.DdcClustersApi
	DdcCustomers *pallets.DdcCustomersApi
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
		SubstrateAPI: substrateApi,
		DdcClusters:  pallets.NewDdcClustersApi(substrateApi),
		DdcCustomers: pallets.NewDdcCustomersApi(substrateApi, meta),
	}, nil
}
