package pallets

import (
	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/cerebellum-network/cere-ddc-sdk-go/blockchain/pkg/ddcprimitives"
)

type AccountsLedger struct {
	Owner     types.AccountID
	Total     types.U128
	Active    types.U128
	Unlocking []UnlockChunk
}

type Bucket struct {
	BucketId  ddcprimitives.BucketId
	OwnerId   types.AccountID
	ClusterId ddcprimitives.ClusterId
}

type BucketDetails struct {
	BucketId ddcprimitives.BucketId
	Amount   types.U128
}

type UnlockChunk struct {
	Value types.U128
	Block types.BlockNumber
}

type Buckets map[ddcprimitives.BucketId]types.Option[Bucket]

type Ledger map[types.AccountID]types.Option[AccountsLedger]

type DdcCustomersApi struct {
	substrateApi *gsrpc.SubstrateAPI
	meta         *types.Metadata
}

func NewDdcCustomersApi(substrateAPI *gsrpc.SubstrateAPI, meta *types.Metadata) *DdcCustomersApi {
	return &DdcCustomersApi{
		substrateAPI,
		meta,
	}
}

func (api *DdcCustomersApi) GetBuckets(bucketId ddcprimitives.BucketId) (types.Option[Bucket], error) {
	maybeBucket := types.NewEmptyOption[Bucket]()

	bytes, err := codec.Encode(bucketId)
	if err != nil {
		return maybeBucket, err
	}

	key, err := types.CreateStorageKey(api.meta, "DdcCustomers", "Buckets", bytes)
	if err != nil {
		return maybeBucket, err
	}

	var bucket Bucket
	ok, err := api.substrateApi.RPC.State.GetStorageLatest(key, &bucket)
	if !ok || err != nil {
		return maybeBucket, err
	}

	maybeBucket.SetSome(bucket)

	return maybeBucket, nil
}

func (api *DdcCustomersApi) GetBucketsCount() (types.U64, error) {
	key, err := types.CreateStorageKey(api.meta, "DdcCustomers", "BucketsCount")
	if err != nil {
		return 0, err
	}

	var bucketsCount types.U64
	ok, err := api.substrateApi.RPC.State.GetStorageLatest(key, &bucketsCount)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}

	return bucketsCount, nil
}

func (api *DdcCustomersApi) GetLedger(owner types.AccountID) (types.Option[AccountsLedger], error) {
	maybeLedger := types.NewEmptyOption[AccountsLedger]()

	bytes, err := codec.Encode(owner)
	if err != nil {
		return maybeLedger, err
	}

	key, err := types.CreateStorageKey(api.meta, "DdcCustomers", "Ledger", bytes)
	if err != nil {
		return maybeLedger, err
	}

	var accountsLedger AccountsLedger
	ok, err := api.substrateApi.RPC.State.GetStorageLatest(key, &accountsLedger)
	if !ok || err != nil {
		return maybeLedger, err
	}

	maybeLedger.SetSome(accountsLedger)

	return maybeLedger, nil
}
