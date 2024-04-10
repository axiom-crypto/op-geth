package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	//"sync"
	"time"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/console/prompt"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/internal/debug"
	"github.com/ethereum/go-ethereum/internal/flags"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"go.uber.org/automaxprocs/maxprocs"
	"github.com/urfave/cli/v2"
)

var (
	DataDirFlag = &flags.DirectoryFlag{
		Name:     "datadir",
		Usage:    "Data directory for the databases and keystore",
		Value:    flags.DirectoryString(node.DefaultDataDir()),
		Category: flags.EthCategory,
	}
	Analyse = &cli.StringFlag{
		Name:     "analyse",
		Usage:    "Which one, dude?",
		Category: flags.EthCategory,
	}
	cliFlags = []cli.Flag{
		DataDirFlag,
		Analyse,
	}

)

var app = flags.NewApp("The CLI")

var emptyRoot = common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")

func init() {
	app.Action = deth
	app.Copyright = "Nah."
	app.Flags = flags.Merge(
		cliFlags,
	)

	app.Before = func(ctx *cli.Context) error {
		maxprocs.Set()
		flags.MigrateGlobalFlags(ctx)
		if err := debug.Setup(ctx); err != nil {
			return err
		}
		flags.CheckEnvVars(ctx, app.Flags, "GETH")
		return nil
	}
	app.After = func(ctx *cli.Context) error {
		debug.Exit()
		prompt.Stdin.Close()
		return nil
	}
}

func main() {
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func deth(ctx *cli.Context) error {
	option := ctx.String("analyse")
	datadir := ctx.String("datadir")

	db, err := rawdb.Open(rawdb.OpenOptions{
		Type:      "",
		Directory: datadir + "/geth" + "/chaindata",
		Namespace: "",
		Cache:     2048,
		Handles:   1024,
		ReadOnly:  true,
	})

	if err != nil {
		fmt.Println("OOPS", err)
		return nil
	}

	b := rawdb.ReadHeadBlock(db)
	fmt.Println("HEAD Block: ", b.Hash())
	fmt.Println("Number: ", b.Number())
	fmt.Println("Analyse: ", option)

	if option == "access_list" {
		analyseAccessLists(db)
	} else if option == "storage_trie" {
		analyseStorageTries(db)
	} else {
		fmt.Println("account_trie, storage_trie, or access_list")
	}

	return nil
}

func analyseStorageTries(db ethdb.Database) {
	statedb := state.NewDatabase(db)
	triedb := statedb.TrieDB()

	b := rawdb.ReadHeadBlock(db)

	analyseTrie(b.Root(), statedb, triedb, byte(0xbe))
	//var wg sync.WaitGroup
	//for i := 0; i < 256; i++ {
	//	wg.Add(1)

	//	go func(r common.Hash, s state.Database, t *trie.Database, i byte) {
	//		defer wg.Done()
	//		analyseTrie(r, s, t, i)
	//	}(b.Root(), statedb, triedb, byte(i))
	//}
	//wg.Wait()
}

func analyseTrie(trieRoot common.Hash, statedb state.Database, triedb *trie.Database, index byte) {
	t, err := state.Database.OpenTrie(statedb, trieRoot)

	if err != nil {
		fmt.Println("Couldn't read trie! ", err)
		return
	}

	histStateTrieDepths := Histogram[int]{
		title: "State Trie - Depths",
		buckets: map[int]int64{},
		total: 0,
	}
	histStorageTrieDepths := Histogram[int]{
		title: "Storage Trie - Depths",
		buckets: map[int]int64{},
		total: 0,
	}
	histStorageTriesNumSlots := Histogram[int64]{
		title: "Storage Trie - Number of used slots",
		buckets: map[int64]int64{},
		total: 0,
	}

	startNode := [32]byte{}
	startNode[0] = index

	iter, err := t.NodeIterator(startNode[:])

	if err != nil {
		fmt.Println("Couldn't iterate Trie!", err)
		return
	}
	var leafNodes int
	var storageTries int64
	lastReport := time.Now()

	for iter.Next(true) {
		if iter.Leaf() {
			if iter.LeafKey()[0] != index {
				break
			}
			leafNodes++

			leafProof := iter.LeafProof()
			histStateTrieDepths.Observe(len(leafProof))

			var acc types.StateAccount
			if err := rlp.DecodeBytes(iter.LeafBlob(), &acc); err != nil {
				fmt.Println("ERROR: invalid account encountered during traversal: %s", err)
				return
			}
			if acc.Root != emptyRoot {
				storageTries++
				c := make(chan Histogram[int])

				for i := 0; i < 256; i++ {
					go func(r common.Hash, l common.Hash, tdb *trie.Database, a types.StateAccount, c chan Histogram[int], i byte) {
						processStorage(r, l, tdb, a, c, i)
					}(trieRoot, common.BytesToHash(iter.LeafKey()), triedb, acc, c, byte(i))
				}
			        for i := 0; i < 256; i++ {
			                result := <-c
					histStorageTriesNumSlots.Observe(result.total)
					for key := range result.sortedKeys() {
						if result.buckets[key] > 0 {
							histStorageTrieDepths.buckets[key] += result.buckets[key]
						}
					}
					histStorageTrieDepths.total += result.total
			        }
				if time.Since(lastReport) > time.Minute*1 {
					stateTrieReport(leafNodes, storageTries, histStateTrieDepths, histStorageTrieDepths, histStorageTriesNumSlots, startNode[:])
					lastReport = time.Now()
				}
			}
		}

		if time.Since(lastReport) > time.Minute*1 {
			stateTrieReport(leafNodes, storageTries, histStateTrieDepths, histStorageTrieDepths, histStorageTriesNumSlots, startNode[:])
			lastReport = time.Now()
		}
	}

	if iter.Error() != nil {
	    fmt.Println("Error iterating trie: ", iter.Error())
	    return
	}

	fmt.Println("Dumping final report.")
	stateTrieReport(leafNodes, storageTries, histStateTrieDepths, histStorageTrieDepths, histStorageTriesNumSlots, startNode[:])
}

func processStorage(trieRoot common.Hash, leafKey common.Hash, triedb *trie.Database, acc types.StateAccount, c chan Histogram[int], index byte) {
	lastReport := time.Now()
	histStorageTrieDepths := Histogram[int]{
		title: "Storage Trie - Depths",
		buckets: map[int]int64{},
		total: 0,
	}

	id := trie.StorageTrieID(trieRoot, leafKey, acc.Root)
	storageTrie, err := trie.NewStateTrie(id, triedb)
	if err != nil {
		fmt.Println("ERROR: failed to open storage trie: %s", err)
		c <- histStorageTrieDepths
		return
	}

	startNode := [32]byte{}
	startNode[0] = index
	storageIter, err := storageTrie.NodeIterator(startNode[:])

	var numSlots int64
	for storageIter.Next(true) {
		if storageIter.Leaf() {
			numSlots++
			if storageIter.LeafKey()[0] != index {
				break
			}
			leafProof := storageIter.LeafProof()
			histStorageTrieDepths.Observe(len(leafProof))
		}
		if time.Since(lastReport) > time.Minute*1 {
			fmt.Println("Processing storage shard %s-%d (%d)", acc.Root, index, numSlots)
			lastReport = time.Now()
		}
	}
	if storageIter.Error() != nil {
		fmt.Println("ERROR: Failed to traverse storage trie: %s", err)
		c <- histStorageTrieDepths
		return
	}

	c <- histStorageTrieDepths
}

func stateTrieReport(leafNodes int, storageTries int64, histStateTrieDepths Histogram[int], histStorageTrieDepths Histogram[int], histStorageTriesNumSlots Histogram[int64], startFrom []byte) {
	start := hex.EncodeToString(startFrom)

	// State Trie stdout reports.
	fmt.Printf("(%s) Walked %d (EOA + SC) accounts:\n", start, leafNodes)

	// Storage tries stdout reports.
	fmt.Printf("(%s) Walked %d Storage Tries:\n", start, storageTries)

	// State Trie.
	histStateTrieDepths.ToCSV("statetrie_depth_" + start + ".csv")

	// Storage Tries.
	histStorageTrieDepths.ToCSV("storagetrie_depth_" + start + ".csv")
	histStorageTriesNumSlots.ToCSV("storagetrie_numslots_" + start + ".csv")

}

func analyseAccessLists(db ethdb.Database) {
	b := rawdb.ReadHeadBlock(db)

	fmt.Println("TxType, TxHash, AccessListRLPLen")
	for {
		blockNumber := b.NumberU64()

		for _, tx := range b.Transactions() {
			accessList := tx.AccessList()
			buffer := new(bytes.Buffer)
			rlp.Encode(buffer, accessList)

			fmt.Println(tx.Type(), ", ", tx.Hash(), ", ", buffer.Len())
		}


		parent := b.ParentHash()

		if blockNumber == 0 {
			break
		} else {
			b = rawdb.ReadBlock(db, parent, blockNumber - 1)
		}
	}
}
