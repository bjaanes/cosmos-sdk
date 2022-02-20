package iavl

import (
	"errors"
	"fmt"
	"github.com/tendermint/tendermint/libs/log"
	"io"
	"time"

	ics23 "github.com/confio/ics23/go"
	"github.com/cosmos/iavl"
	abci "github.com/tendermint/tendermint/abci/types"
	tmcrypto "github.com/tendermint/tendermint/proto/tendermint/crypto"
	dbm "github.com/tendermint/tm-db"

	"github.com/cosmos/cosmos-sdk/store/cachekv"
	"github.com/cosmos/cosmos-sdk/store/listenkv"
	"github.com/cosmos/cosmos-sdk/store/tracekv"
	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/kv"
)

const (
	DefaultIAVLCacheSize = 500000
)

var (
	_ types.KVStore                 = (*Store)(nil)
	_ types.CommitStore             = (*Store)(nil)
	_ types.CommitKVStore           = (*Store)(nil)
	_ types.Queryable               = (*Store)(nil)
	_ types.StoreWithInitialVersion = (*Store)(nil)
)

// Store Implements types.KVStore and CommitKVStore.
type Store struct {
	tree Tree
}

// LoadStore returns an IAVL Store as a CommitKVStore. Internally, it will load the
// store's version (id) from the provided DB. An error is returned if the version
// fails to load, or if called with a positive version on an empty tree.
func LoadStore(db dbm.DB, logger log.Logger, key types.StoreKey, id types.CommitID, lazyLoading bool, cacheSize int) (types.CommitKVStore, error) {
	return LoadStoreWithInitialVersion(db, logger, key, id, lazyLoading, 0, cacheSize)
}

// LoadStoreWithInitialVersion returns an IAVL Store as a CommitKVStore setting its initialVersion
// to the one given. Internally, it will load the store's version (id) from the
// provided DB. An error is returned if the version fails to load, or if called with a positive
// version on an empty tree.
func LoadStoreWithInitialVersion(db dbm.DB, logger log.Logger, key types.StoreKey, id types.CommitID, lazyLoading bool, initialVersion uint64, cacheSize int) (types.CommitKVStore, error) {
	tree, err := iavl.NewMutableTreeWithOpts(db, cacheSize, &iavl.Options{InitialVersion: initialVersion})
	if err != nil {
		return nil, err
	}

	if tree.IsUpgradeable() && logger != nil {
		logger.Info(
			"Upgrading IAVL storage for faster queries + execution on live state. This may take a while",
			"store_key", key.String(),
			"version", initialVersion,
			"commit", fmt.Sprintf("%X", id),
			"is_lazy", lazyLoading,
		)
	}

	if lazyLoading {
		_, err = tree.LazyLoadVersion(id.Version)
	} else {
		_, err = tree.LoadVersion(id.Version)
	}

	if err != nil {
		return nil, err
	}

	if logger != nil {
		logger.Debug("Finished loading IAVL tree")
	}

	return &Store{
		tree: tree,
	}, nil
}

// UnsafeNewStore returns a reference to a new IAVL Store with a given mutable
// IAVL tree reference. It should only be used for testing purposes.
//
// CONTRACT: The IAVL tree should be fully loaded.
// CONTRACT: PruningOptions passed in as argument must be the same as pruning options
// passed into iavl.MutableTree
func UnsafeNewStore(tree *iavl.MutableTree) *Store {
	return &Store{
		tree: tree,
	}
}

// GetImmutable returns a reference to a new store backed by an immutable IAVL
// tree at a specific version (height) without any pruning options. This should
// be used for querying and iteration only. If the version does not exist or has
// been pruned, an empty immutable IAVL tree will be used.
// Any mutable operations executed will result in a panic.
func (st *Store) GetImmutable(version int64) (*Store, error) {
	if !st.VersionExists(version) {
		return &Store{tree: &immutableTree{&iavl.ImmutableTree{}}}, nil
	}

	iTree, err := st.tree.GetImmutable(version)
	if err != nil {
		return nil, err
	}

	return &Store{
		tree: &immutableTree{iTree},
	}, nil
}

// Commit commits the current store state and returns a CommitID with the new
// version and hash.
func (st *Store) Commit() types.CommitID {
	defer telemetry.MeasureSince(time.Now(), "store", "iavl", "commit")

	hash, version, err := st.tree.SaveVersion()
	if err != nil {
		panic(err)
	}

	return types.CommitID{
		Version: version,
		Hash:    hash,
	}
}

// LastCommitID implements Committer.
func (st *Store) LastCommitID() types.CommitID {
	return types.CommitID{
		Version: st.tree.Version(),
		Hash:    st.tree.Hash(),
	}
}

// SetPruning panics as pruning options should be provided at initialization
// since IAVl accepts pruning options directly.
func (st *Store) SetPruning(_ types.PruningOptions) {
	panic("cannot set pruning options on an initialized IAVL store")
}

// SetPruning panics as pruning options should be provided at initialization
// since IAVl accepts pruning options directly.
func (st *Store) GetPruning() types.PruningOptions {
	panic("cannot get pruning options on an initialized IAVL store")
}

// VersionExists returns whether or not a given version is stored.
func (st *Store) VersionExists(version int64) bool {
	return st.tree.VersionExists(version)
}

// Implements Store.
func (st *Store) GetStoreType() types.StoreType {
	return types.StoreTypeIAVL
}

// Implements Store.
func (st *Store) CacheWrap() types.CacheWrap {
	return cachekv.NewStore(st)
}

// CacheWrapWithTrace implements the Store interface.
func (st *Store) CacheWrapWithTrace(w io.Writer, tc types.TraceContext) types.CacheWrap {
	return cachekv.NewStore(tracekv.NewStore(st, w, tc))
}

// CacheWrapWithListeners implements the CacheWrapper interface.
func (st *Store) CacheWrapWithListeners(storeKey types.StoreKey, listeners []types.WriteListener) types.CacheWrap {
	return cachekv.NewStore(listenkv.NewStore(st, storeKey, listeners))
}

// Implements types.KVStore.
func (st *Store) Set(key, value []byte) {
	types.AssertValidKey(key)
	types.AssertValidValue(value)
	st.tree.Set(key, value)
}

// Implements types.KVStore.
func (st *Store) Get(key []byte) []byte {
	defer telemetry.MeasureSince(time.Now(), "store", "iavl", "get")
	return st.tree.Get(key)
}

// Implements types.KVStore.
func (st *Store) Has(key []byte) (exists bool) {
	defer telemetry.MeasureSince(time.Now(), "store", "iavl", "has")
	return st.tree.Has(key)
}

// Implements types.KVStore.
func (st *Store) Delete(key []byte) {
	defer telemetry.MeasureSince(time.Now(), "store", "iavl", "delete")
	st.tree.Remove(key)
}

// DeleteVersions deletes a series of versions from the MutableTree. An error
// is returned if any single version is invalid or the delete fails. All writes
// happen in a single batch with a single commit.
func (st *Store) DeleteVersions(versions ...int64) error {
	return st.tree.DeleteVersions(versions...)
}

// Implements types.KVStore.
// CONTRACT: Caller must release the iavlIterator, as each one creates a new
// goroutine.
// CONTRACT: There must be no writes to the store while an iterator is not closed.
func (st *Store) Iterator(start, end []byte) types.Iterator {
	return st.tree.Iterator(start, end, true)
}

// Implements types.KVStore.
// CONTRACT: Caller must release the iavlIterator, as each one creates a new
// goroutine.
// CONTRACT: There must be no writes to the store while an iterator is not closed.
func (st *Store) ReverseIterator(start, end []byte) types.Iterator {
	return st.tree.Iterator(start, end, false)
}

// SetInitialVersion sets the initial version of the IAVL tree. It is used when
// starting a new chain at an arbitrary height.
func (st *Store) SetInitialVersion(version int64) {
	st.tree.SetInitialVersion(uint64(version))
}

// Exports the IAVL store at the given version, returning an iavl.Exporter for the tree.
func (st *Store) Export(version int64) (*iavl.Exporter, error) {
	istore, err := st.GetImmutable(version)
	if err != nil {
		return nil, fmt.Errorf("iavl export failed for version %v: %w", version, err)
	}
	tree, ok := istore.tree.(*immutableTree)
	if !ok || tree == nil {
		return nil, fmt.Errorf("iavl export failed: unable to fetch tree for version %v", version)
	}
	return tree.Export(), nil
}

// Import imports an IAVL tree at the given version, returning an iavl.Importer for importing.
func (st *Store) Import(version int64) (*iavl.Importer, error) {
	tree, ok := st.tree.(*iavl.MutableTree)
	if !ok {
		return nil, errors.New("iavl import failed: unable to find mutable tree")
	}
	return tree.Import(version)
}

// Handle gatest the latest height, if height is 0
func getHeight(tree Tree, req abci.RequestQuery) int64 {
	height := req.Height
	if height == 0 {
		latest := tree.Version()
		if tree.VersionExists(latest - 1) {
			height = latest - 1
		} else {
			height = latest
		}
	}
	return height
}

// Query implements ABCI interface, allows queries
//
// by default we will return from (latest height -1),
// as we will have merkle proofs immediately (header height = data height + 1)
// If latest-1 is not present, use latest (which must be present)
// if you care to have the latest data to see a tx results, you must
// explicitly set the height you want to see
func (st *Store) Query(req abci.RequestQuery) (res abci.ResponseQuery) {
	defer telemetry.MeasureSince(time.Now(), "store", "iavl", "query")

	if len(req.Data) == 0 {
		return sdkerrors.QueryResult(sdkerrors.Wrap(sdkerrors.ErrTxDecode, "query cannot be zero length"))
	}

	tree := st.tree

	// store the height we chose in the response, with 0 being changed to the
	// latest height
	res.Height = getHeight(tree, req)

	switch req.Path {
	case "/key": // get by key
		key := req.Data // data holds the key bytes

		res.Key = key
		if !st.VersionExists(res.Height) {
			res.Log = iavl.ErrVersionDoesNotExist.Error()
			break
		}

		res.Value = tree.GetVersioned(key, res.Height)
		if !req.Prove {
			break
		}

		// Continue to prove existence/absence of value
		// Must convert store.Tree to iavl.MutableTree with given version to use in CreateProof
		iTree, err := tree.GetImmutable(res.Height)
		if err != nil {
			// sanity check: If value for given version was retrieved, immutable tree must also be retrievable
			panic(fmt.Sprintf("version exists in store but could not retrieve corresponding versioned tree in store, %s", err.Error()))
		}
		mtree := &iavl.MutableTree{
			ImmutableTree: iTree,
		}

		// get proof from tree and convert to merkle.Proof before adding to result
		res.ProofOps = getProofFromTree(mtree, req.Data, res.Value != nil)

	case "/subspace":
		pairs := kv.Pairs{
			Pairs: make([]kv.Pair, 0),
		}

		subspace := req.Data
		res.Key = subspace

		iterator := types.KVStorePrefixIterator(st, subspace)
		for ; iterator.Valid(); iterator.Next() {
			pairs.Pairs = append(pairs.Pairs, kv.Pair{Key: iterator.Key(), Value: iterator.Value()})
		}
		iterator.Close()

		bz, err := pairs.Marshal()
		if err != nil {
			panic(fmt.Errorf("failed to marshal KV pairs: %w", err))
		}

		res.Value = bz

	default:
		return sdkerrors.QueryResult(sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "unexpected query path: %v", req.Path))
	}

	return res
}

// Takes a MutableTree, a key, and a flag for creating existence or absence proof and returns the
// appropriate merkle.Proof. Since this must be called after querying for the value, this function should never error
// Thus, it will panic on error rather than returning it
func getProofFromTree(tree *iavl.MutableTree, key []byte, exists bool) *tmcrypto.ProofOps {
	var (
		commitmentProof *ics23.CommitmentProof
		err             error
	)

	if exists {
		// value was found
		commitmentProof, err = tree.GetMembershipProof(key)
		if err != nil {
			// sanity check: If value was found, membership proof must be creatable
			panic(fmt.Sprintf("unexpected value for empty proof: %s", err.Error()))
		}
	} else {
		// value wasn't found
		commitmentProof, err = tree.GetNonMembershipProof(key)
		if err != nil {
			// sanity check: If value wasn't found, nonmembership proof must be creatable
			panic(fmt.Sprintf("unexpected error for nonexistence proof: %s", err.Error()))
		}
	}

	op := types.NewIavlCommitmentOp(key, commitmentProof)
	return &tmcrypto.ProofOps{Ops: []tmcrypto.ProofOp{op.ProofOp()}}
}

//----------------------------------------

// Implements types.Iterator.
type iavlIterator struct {
	dbm.Iterator
}

var _ types.Iterator = (*iavlIterator)(nil)
