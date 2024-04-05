package main

import (
	"bytes"
	"fmt"
	"os"
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
		Cache:     1024 * 50 / 100,
		Handles:   256,
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
		analyseStorageTries(db, true)
	} else if option == "account_trie" {
		analyseAccountTries(db)
	} else if option == "accounts" {
		analyseStorageTries(db, false)
	} else {
		fmt.Println("account_trie, storage_trie, or access_list")
	}

	return nil
}

func analyseAccountTries(db ethdb.Database) {
	statedb := state.NewDatabase(db)

	b := rawdb.ReadHeadBlock(db)
	root := b.Root()
	trie, err := statedb.OpenTrie(root)

	if err != nil {
		fmt.Println("Failed to open Trie!", err)
		return
	}

	analyseAccounts(root, statedb, trie)
}

func analyseAccounts(trieRoot common.Hash, statedb state.Database, trie state.Trie) {
	lastReport := time.Now()

	_addresses := [...]string{
		"0x4200000000000000000000000000000000000016",
		"0x4200000000000000000000000000000000000006",
		"0xbaed383ede0e5d9d72430661f3285daa77e9439f",
		"0x3304e22ddaa22bcdc5fca2269b418046ae7b566a",
		"0xcf205808ed36593aa40a44f10c7f7c2f67d4a4d4",
		"0x831be9e08185eba7d88aab1efc059336babef430",
		"0xfd92f4e91d54b9ef91cc3f97c011a6af0c2a7eda",
		"0x224d8fd7ab6ad4c6eb4611ce56ef35dec2277f03",
		"0x5d639027789bd9d53c1a32dc1cb18e6f1a16234c",
		"0x9c3631dde5c8316be5b7554b0ccd2631c15a9a05",
		"0xecb03b9a0e7f7b5e261d3ef752865af6621a54fe",
		"0x3a972d7b008816abb41287cdf93cb1eb0236eb68",
		"0x79bf8fe8f0c6986b447a9394852072542972ce9c",
		"0x0977250dbefe33086cebfb73970e0473c592fc54",
		"0x4869892c8088d9057ad7d0e5189ae8c3278f8877",
		"0x739120ade7ed878fca5bbdb806263a8258fe2360",
		"0xf1b98463d6574c998f03e6edf6d7b4e5f917f915",
		"0x4e3ae00e8323558fa5cac04b152238924aa31b60",
		"0x7bf90111ad7c22bec9e9dff8a01a44713cc1b1b6",
		"0xbfdb66a6783a6dc757f539139c68bed75eb491c8",
		"0xc32df201476bb79318c32fd696b2ccdcc5f9a909",
		"0x7a560269480ef38b885526c8bbecdc4686d8bf7a",
		"0x5593b57d06458f62b1a4f31a997945a3b51fc2fd",
		"0x1ebfa746da64b049937988c51e77a138250846ca",
	}

	addresses := make([]common.Address, len(_addresses))
	for i, addr := range _addresses {
		addresses[i] = common.HexToAddress(addr)
	}

	for _, addr := range addresses {
		fmt.Println(addr)
		// Histograms for Storage Tries.
		histStorageTrieDepths := Histogram[int]{
			title: "Storage Trie - Depths - " + addr.String(),
			buckets: map[int]int64{},
			total: 0,
		}

		account, _ := trie.GetAccount(addr)

		if account == nil {
			fmt.Println("SKIP account")
			continue
		}

		storageTrie, err := statedb.OpenStorageTrie(trieRoot, addr, account.Root, nil)

		if err != nil {
			fmt.Println("failed to open storage trie: %s", err)
			return
		}

		var storageTriesNumSlots int64
		storageIter, _ := storageTrie.NodeIterator(nil)
		for storageIter.Next(true) {
			if storageIter.Leaf() {
				storageTriesNumSlots += 1
				leafProof := storageIter.LeafProof()
				histStorageTrieDepths.Observe(len(leafProof))
			}

			if time.Since(lastReport) > time.Minute {

				// // Storage tries stdout reports.
				fmt.Printf("Walked %d Storage Slots for %s:\n", storageTriesNumSlots, addr.String())
				histStorageTrieDepths.Print(os.Stdout)

				fmt.Printf("-----\n\n")

				lastReport = time.Now()
			}
		}

		if storageIter.Error() != nil {
			fmt.Println("Failed to traverse storage trie: %s", err)
			return
		}

		fmt.Println("Finished walking storage trie for", addr.String())
		fmt.Println("Total storage slots:", storageTriesNumSlots)
		fmt.Println("Storage trie depth histogram:")
		histStorageTrieDepths.Print(os.Stdout)
		fmt.Println("-----\n")
		histStorageTrieDepths.ToCSV("storage_trie_" + addr.String() + ".csv")
	}
}

func analyseStorageTries(db ethdb.Database, analyseAccountStorage bool) {
	statedb := state.NewDatabase(db)
	triedb := statedb.TrieDB()

	b := rawdb.ReadHeadBlock(db)

	analyseTrie(b.Root(), statedb, triedb, analyseAccountStorage)
}

func analyseTrie(trieRoot common.Hash, statedb state.Database, triedb *trie.Database, analyseAccountStorage bool) {
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

	iter, err := t.NodeIterator(nil)

	if err != nil {
		fmt.Println("Couldn't iterate Trie!", err)
		return
	}
	var leafNodes int
	var storageTries int64
	lastReport := time.Now()

	for iter.Next(true) {
		if iter.Leaf() {
			leafNodes++

			leafProof := iter.LeafProof()
			histStateTrieDepths.Observe(len(leafProof))

			var acc types.StateAccount
			if err := rlp.DecodeBytes(iter.LeafBlob(), &acc); err != nil {
				fmt.Println("ERROR: invalid account encountered during traversal: %s", err)
				return
			}
			if acc.Root != emptyRoot && analyseAccountStorage {
				storageTries++
				id := trie.StorageTrieID(trieRoot, common.BytesToHash(iter.LeafKey()), acc.Root)
				storageTrie, err := trie.NewStateTrie(id, triedb)
				if err != nil {
					fmt.Println("ERROR: failed to open storage trie: %s", err)
					return
				}

				var storageTriesNumSlots int64
				storageIter, _ := storageTrie.NodeIterator(nil)
				for storageIter.Next(true) {
					if storageIter.Leaf() {
						storageTriesNumSlots += 1
						leafProof := storageIter.LeafProof()
						histStorageTrieDepths.Observe(len(leafProof))
					}
				}
				histStorageTriesNumSlots.Observe(storageTriesNumSlots)

				if storageIter.Error() != nil {
					fmt.Println("ERROR: Failed to traverse storage trie: %s", err)
					return
				}
			}
		}

		if time.Since(lastReport) > time.Minute*1 {
			stateTrieReport(leafNodes, storageTries, histStateTrieDepths, histStorageTrieDepths, histStorageTriesNumSlots)
			lastReport = time.Now()
		}
	}

	if iter.Error() != nil {
	    fmt.Println("Error iterating trie: ", iter.Error())
	    return
	}

	fmt.Println("Dumping final report.")
	stateTrieReport(leafNodes, storageTries, histStateTrieDepths, histStorageTrieDepths, histStorageTriesNumSlots)
}

func stateTrieReport(leafNodes int, storageTries int64, histStateTrieDepths Histogram[int], histStorageTrieDepths Histogram[int], histStorageTriesNumSlots Histogram[int64]) {
	// State Trie stdout reports.
	fmt.Printf("Walked %d (EOA + SC) accounts:\n", leafNodes)
	histStateTrieDepths.Print(os.Stdout)
	fmt.Println()

	// Storage tries stdout reports.
	fmt.Printf("Walked %d Storage Tries:\n", storageTries)
	histStorageTrieDepths.Print(os.Stdout)
	histStorageTriesNumSlots.Print(os.Stdout)

	fmt.Printf("-----\n\n")

	// State Trie.
	histStateTrieDepths.ToCSV("statetrie_depth.csv")

	// Storage Tries.
	histStorageTrieDepths.ToCSV("storagetrie_depth.csv")
	histStorageTriesNumSlots.ToCSV("storagetrie_numslots.csv")

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
