package itests

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/itests/kit"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/extern/sector-storage/mock"
	"github.com/filecoin-project/specs-storage/storage"

	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/impl"
)

func TestWindowedPost(t *testing.T) {
	kit.QuietMiningLogs()

	var (
		blocktime = 2 * time.Millisecond
		nSectors  = 10
	)

	for _, height := range []abi.ChainEpoch{
		-1,   // before
		162,  // while sealing
		5000, // while proving
	} {
		height := height // copy to satisfy lints
		t.Run(fmt.Sprintf("upgrade-%d", height), func(t *testing.T) {
			testWindowPostUpgrade(t, kit.MockMinerBuilder, blocktime, nSectors, height)
		})
	}
}

func testWindowPostUpgrade(t *testing.T, b kit.APIBuilder, blocktime time.Duration, nSectors int, upgradeHeight abi.ChainEpoch) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n, sn := b(t, []kit.FullNodeOpts{kit.FullNodeWithLatestActorsAt(upgradeHeight)}, kit.OneMiner)

	client := n[0].FullNode.(*impl.FullNodeAPI)
	miner := sn[0]

	addrinfo, err := client.NetAddrsListen(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := miner.NetConnect(ctx, addrinfo); err != nil {
		t.Fatal(err)
	}
	build.Clock.Sleep(time.Second)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			build.Clock.Sleep(blocktime)
			if err := sn[0].MineOne(ctx, kit.MineNext); err != nil {
				if ctx.Err() != nil {
					// context was canceled, ignore the error.
					return
				}
				t.Error(err)
			}
		}
	}()
	defer func() {
		cancel()
		<-done
	}()

	kit.PledgeSectors(t, ctx, miner, nSectors, 0, nil)

	maddr, err := miner.ActorAddress(ctx)
	require.NoError(t, err)

	di, err := client.StateMinerProvingDeadline(ctx, maddr, types.EmptyTSK)
	require.NoError(t, err)

	mid, err := address.IDFromAddress(maddr)
	require.NoError(t, err)

	fmt.Printf("Running one proving period\n")
	fmt.Printf("End for head.Height > %d\n", di.PeriodStart+di.WPoStProvingPeriod+2)

	for {
		head, err := client.ChainHead(ctx)
		require.NoError(t, err)

		if head.Height() > di.PeriodStart+di.WPoStProvingPeriod+2 {
			fmt.Printf("Now head.Height = %d\n", head.Height())
			break
		}
		build.Clock.Sleep(blocktime)
	}

	p, err := client.StateMinerPower(ctx, maddr, types.EmptyTSK)
	require.NoError(t, err)

	ssz, err := miner.ActorSectorSize(ctx, maddr)
	require.NoError(t, err)

	require.Equal(t, p.MinerPower, p.TotalPower)
	require.Equal(t, p.MinerPower.RawBytePower, types.NewInt(uint64(ssz)*uint64(nSectors+kit.GenesisPreseals)))

	fmt.Printf("Drop some sectors\n")

	// Drop 2 sectors from deadline 2 partition 0 (full partition / deadline)
	{
		parts, err := client.StateMinerPartitions(ctx, maddr, 2, types.EmptyTSK)
		require.NoError(t, err)
		require.Greater(t, len(parts), 0)

		secs := parts[0].AllSectors
		n, err := secs.Count()
		require.NoError(t, err)
		require.Equal(t, uint64(2), n)

		// Drop the partition
		err = secs.ForEach(func(sid uint64) error {
			return miner.StorageMiner.(*impl.StorageMinerAPI).IStorageMgr.(*mock.SectorMgr).MarkCorrupted(storage.SectorRef{
				ID: abi.SectorID{
					Miner:  abi.ActorID(mid),
					Number: abi.SectorNumber(sid),
				},
			}, true)
		})
		require.NoError(t, err)
	}

	var s storage.SectorRef

	// Drop 1 sectors from deadline 3 partition 0
	{
		parts, err := client.StateMinerPartitions(ctx, maddr, 3, types.EmptyTSK)
		require.NoError(t, err)
		require.Greater(t, len(parts), 0)

		secs := parts[0].AllSectors
		n, err := secs.Count()
		require.NoError(t, err)
		require.Equal(t, uint64(2), n)

		// Drop the sector
		sn, err := secs.First()
		require.NoError(t, err)

		all, err := secs.All(2)
		require.NoError(t, err)
		fmt.Println("the sectors", all)

		s = storage.SectorRef{
			ID: abi.SectorID{
				Miner:  abi.ActorID(mid),
				Number: abi.SectorNumber(sn),
			},
		}

		err = miner.StorageMiner.(*impl.StorageMinerAPI).IStorageMgr.(*mock.SectorMgr).MarkFailed(s, true)
		require.NoError(t, err)
	}

	di, err = client.StateMinerProvingDeadline(ctx, maddr, types.EmptyTSK)
	require.NoError(t, err)

	fmt.Printf("Go through another PP, wait for sectors to become faulty\n")
	fmt.Printf("End for head.Height > %d\n", di.PeriodStart+di.WPoStProvingPeriod+2)

	for {
		head, err := client.ChainHead(ctx)
		require.NoError(t, err)

		if head.Height() > di.PeriodStart+(di.WPoStProvingPeriod)+2 {
			fmt.Printf("Now head.Height = %d\n", head.Height())
			break
		}

		build.Clock.Sleep(blocktime)
	}

	p, err = client.StateMinerPower(ctx, maddr, types.EmptyTSK)
	require.NoError(t, err)

	require.Equal(t, p.MinerPower, p.TotalPower)

	sectors := p.MinerPower.RawBytePower.Uint64() / uint64(ssz)
	require.Equal(t, nSectors+kit.GenesisPreseals-3, int(sectors)) // -3 just removed sectors

	fmt.Printf("Recover one sector\n")

	err = miner.StorageMiner.(*impl.StorageMinerAPI).IStorageMgr.(*mock.SectorMgr).MarkFailed(s, false)
	require.NoError(t, err)

	di, err = client.StateMinerProvingDeadline(ctx, maddr, types.EmptyTSK)
	require.NoError(t, err)

	fmt.Printf("End for head.Height > %d\n", di.PeriodStart+di.WPoStProvingPeriod+2)

	for {
		head, err := client.ChainHead(ctx)
		require.NoError(t, err)

		if head.Height() > di.PeriodStart+di.WPoStProvingPeriod+2 {
			fmt.Printf("Now head.Height = %d\n", head.Height())
			break
		}

		build.Clock.Sleep(blocktime)
	}

	p, err = client.StateMinerPower(ctx, maddr, types.EmptyTSK)
	require.NoError(t, err)

	require.Equal(t, p.MinerPower, p.TotalPower)

	sectors = p.MinerPower.RawBytePower.Uint64() / uint64(ssz)
	require.Equal(t, nSectors+kit.GenesisPreseals-2, int(sectors)) // -2 not recovered sectors

	// pledge a sector after recovery

	kit.PledgeSectors(t, ctx, miner, 1, nSectors, nil)

	{
		// Wait until proven.
		di, err = client.StateMinerProvingDeadline(ctx, maddr, types.EmptyTSK)
		require.NoError(t, err)

		waitUntil := di.PeriodStart + di.WPoStProvingPeriod + 2
		fmt.Printf("End for head.Height > %d\n", waitUntil)

		for {
			head, err := client.ChainHead(ctx)
			require.NoError(t, err)

			if head.Height() > waitUntil {
				fmt.Printf("Now head.Height = %d\n", head.Height())
				break
			}
		}
	}

	p, err = client.StateMinerPower(ctx, maddr, types.EmptyTSK)
	require.NoError(t, err)

	require.Equal(t, p.MinerPower, p.TotalPower)

	sectors = p.MinerPower.RawBytePower.Uint64() / uint64(ssz)
	require.Equal(t, nSectors+kit.GenesisPreseals-2+1, int(sectors)) // -2 not recovered sectors + 1 just pledged
}

func TestWindowPostBaseFeeNoBurn(t *testing.T) {
	kit.QuietMiningLogs()

	var (
		blocktime = 2 * time.Millisecond
		nSectors  = 10
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	och := build.UpgradeClausHeight
	build.UpgradeClausHeight = 10

	n, sn := kit.MockMinerBuilder(t, kit.DefaultFullOpts(1), kit.OneMiner)
	client := n[0].FullNode.(*impl.FullNodeAPI)
	miner := sn[0]
	bm := kit.ConnectAndStartMining(t, blocktime, miner, client)
	t.Cleanup(bm.Stop)

	maddr, err := miner.ActorAddress(ctx)
	require.NoError(t, err)

	mi, err := client.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	require.NoError(t, err)

	kit.PledgeSectors(t, ctx, miner, nSectors, 0, nil)
	wact, err := client.StateGetActor(ctx, mi.Worker, types.EmptyTSK)
	require.NoError(t, err)
	en := wact.Nonce

	// wait for a new message to be sent from worker address, it will be a PoSt

waitForProof:
	for {
		wact, err := client.StateGetActor(ctx, mi.Worker, types.EmptyTSK)
		require.NoError(t, err)
		if wact.Nonce > en {
			break waitForProof
		}

		build.Clock.Sleep(blocktime)
	}

	slm, err := client.StateListMessages(ctx, &api.MessageMatch{To: maddr}, types.EmptyTSK, 0)
	require.NoError(t, err)

	pmr, err := client.StateReplay(ctx, types.EmptyTSK, slm[0])
	require.NoError(t, err)

	require.Equal(t, pmr.GasCost.BaseFeeBurn, big.Zero())

	build.UpgradeClausHeight = och
}

func TestWindowPostBaseFeeBurn(t *testing.T) {
	kit.QuietMiningLogs()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	blocktime := 2 * time.Millisecond

	n, sn := kit.MockMinerBuilder(t, []kit.FullNodeOpts{kit.FullNodeWithLatestActorsAt(-1)}, kit.OneMiner)
	client := n[0].FullNode.(*impl.FullNodeAPI)
	miner := sn[0]
	bm := kit.ConnectAndStartMining(t, blocktime, miner, client)
	t.Cleanup(bm.Stop)

	maddr, err := miner.ActorAddress(ctx)
	require.NoError(t, err)

	mi, err := client.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	require.NoError(t, err)

	kit.PledgeSectors(t, ctx, miner, 10, 0, nil)
	wact, err := client.StateGetActor(ctx, mi.Worker, types.EmptyTSK)
	require.NoError(t, err)
	en := wact.Nonce

	// wait for a new message to be sent from worker address, it will be a PoSt

waitForProof:
	for {
		wact, err := client.StateGetActor(ctx, mi.Worker, types.EmptyTSK)
		require.NoError(t, err)
		if wact.Nonce > en {
			break waitForProof
		}

		build.Clock.Sleep(blocktime)
	}

	slm, err := client.StateListMessages(ctx, &api.MessageMatch{To: maddr}, types.EmptyTSK, 0)
	require.NoError(t, err)

	pmr, err := client.StateReplay(ctx, types.EmptyTSK, slm[0])
	require.NoError(t, err)

	require.NotEqual(t, pmr.GasCost.BaseFeeBurn, big.Zero())
}
