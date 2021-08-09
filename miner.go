package main

import (
	"fmt"
	"github.com/fatih/color"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/lib/tablewriter"
	miner2 "github.com/filecoin-project/specs-actors/v2/actors/builtin/miner"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
	"os"
	"strings"
)

var withdrawCmd = &cli.Command{
	Name:      "withdraw",
	Usage:     "矿工提现,例如 withdraw f02420 100, 如果不填写提现金额，则提取miner所有余额",
	ArgsUsage: "[minerId (eg f01000) ] [amount (FIL)]",
	Action: func(cctx *cli.Context) error {
		/**
		1 获取nonce，
		2 签名，使用本地签名
		3 使用fullnodeapi推送消息
		*/

		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			fmt.Printf("连接FULLNODE_API_INFO api失败。%v\n", err)
			return err
		}
		defer closer()

		ctx := lcli.ReqContext(cctx)

		maddr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			fmt.Printf("输入miner ID(%s)不正确。 %v\n", cctx.Args().First(), err)
			return err
		}

		// 用于根据 矿工获取矿工owner账户
		mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
		if err != nil {
			fmt.Printf("输入miner ID(%s)不正确。 %v\n", cctx.Args().First(), err)
			return err
		}

		owner, err := api.StateAccountKey(ctx, mi.Owner, types.EmptyTSK)
		if err != nil {
			fmt.Printf("%s\t%s: error getting account key: %s\n", "owner", owner, err)
			return err
		}
		//fmt.Printf("--->%+v", owner)

		// 获取矿工可用余额
		available, err := api.StateMinerAvailableBalance(ctx, maddr, types.EmptyTSK)
		if err != nil {
			fmt.Printf("读取矿工(%s)余额失败。 %v\n", cctx.Args().First(), err)
			return err
		}

		amount := available
		f, err := types.ParseFIL(cctx.Args().Get(1))
		if err == nil {
			amount = abi.TokenAmount(f)
			//return xerrors.Errorf("parsing 'amount' argument: %w", err)
		} else {
			fmt.Printf("未指定提现 金额，将miner 所有可用余额（%s）提现，\n", types.FIL(amount).Short())
		}

		if amount.GreaterThan(available) {
			fmt.Printf("提现金额%s 超过miner可用余额(%s)，提现失败\n", amount, available)
			return xerrors.Errorf("can't withdraw more funds than available; requested: %s; available: %s", amount, available)
		}

		// 获取nonce
		a, err := api.StateGetActor(ctx, mi.Owner, types.EmptyTSK)
		if err != nil {
			fmt.Printf("读取获取owner地址的nonce失败，err:%v\n", err)
			return err
		}

		params, err := actors.SerializeParams(&miner2.WithdrawBalanceParams{
			AmountRequested: amount, // Default to attempting to withdraw all the extra funds in the miner actor
		})
		if err != nil {
			fmt.Printf("序列化提现参数失败，err:%v\n", err)
			return err
		}

		msg, err := api.GasEstimateMessageGas(ctx, &types.Message{
			To:     maddr,
			From:   owner,
			Value:  types.NewInt(0),
			Method: miner.Methods.WithdrawBalance,
			Nonce:  a.Nonce,
			Params: params,
		}, nil, types.EmptyTSK)
		if err != nil {
			fmt.Printf("评估消息的gas费用失败， err:%v\n", err)
			return xerrors.Errorf("GasEstimateMessageGas error: %w", err)
		}

		fmt.Printf("\n%+v\n", msg)

		// 签名
		signMsg, err := signMessage(msg)
		if err != nil {
			return err
		}

		// 推送消息
		cid, err := api.MpoolPush(ctx, signMsg)
		if err != nil {
			fmt.Printf("推送消息上链失败，err:%v\n", err)
			return err
		}

		fmt.Printf("Requested rewards withdrawal in message %s\n", cid.String())

		return nil
	},
}

var controlSetCmd = &cli.Command{
	Name:      "control-set",
	Usage:     "Set control address(-es)",
	ArgsUsage: "[minerId (eg. f021704)] [...address]",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "really-do-it",
			Usage: "Actually send transaction performing the action",
			Value: false,
		},
	},
	Action: func(cctx *cli.Context) error {
		//nodeApi, closer, err := GetStorageMinerAPI(cctx)
		//if err != nil {
		//	return err
		//}
		//defer closer()

		api, acloser, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer acloser()

		ctx := lcli.ReqContext(cctx)

		//maddr, err := nodeApi.ActorAddress(ctx)
		maddr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		del := map[address.Address]struct{}{}
		existing := map[address.Address]struct{}{}
		for _, controlAddress := range mi.ControlAddresses {
			ka, err := api.StateAccountKey(ctx, controlAddress, types.EmptyTSK)
			if err != nil {
				return err
			}

			del[ka] = struct{}{}
			existing[ka] = struct{}{}
		}

		var toSet []address.Address

		for i, as := range cctx.Args().Tail() {
			a, err := address.NewFromString(as)
			if err != nil {
				return xerrors.Errorf("parsing address %d: %w", i, err)
			}

			ka, err := api.StateAccountKey(ctx, a, types.EmptyTSK)
			if err != nil {
				return err
			}

			// make sure the address exists on chain
			_, err = api.StateLookupID(ctx, ka, types.EmptyTSK)
			if err != nil {
				return xerrors.Errorf("looking up %s: %w", ka, err)
			}

			delete(del, ka)
			toSet = append(toSet, ka)
		}

		for a := range del {
			fmt.Println("Remove", a)
		}
		for _, a := range toSet {
			if _, exists := existing[a]; !exists {
				fmt.Println("Add", a)
			}
		}

		if !cctx.Bool("really-do-it") {
			fmt.Println("Pass --really-do-it to actually execute this action")
			return nil
		}

		cwp := &miner2.ChangeWorkerAddressParams{
			NewWorker:       mi.Worker,
			NewControlAddrs: toSet,
		}

		sp, err := actors.SerializeParams(cwp)
		if err != nil {
			return xerrors.Errorf("serializing params: %w", err)
		}

		// 获取nonce
		a, err := api.StateGetActor(ctx, mi.Owner, types.EmptyTSK)
		if err != nil {
			fmt.Printf("读取获取owner地址的nonce失败，err:%v\n", err)
			return err
		}

		owner, err := api.StateAccountKey(ctx, mi.Owner, types.EmptyTSK)
		if err != nil {
			fmt.Printf("%s\t%s: error getting account key: %s\n", "owner", owner, err)
			return err
		}

		msg, err := api.GasEstimateMessageGas(ctx, &types.Message{
			To:     maddr,
			From:   owner,
			Value:  big.Zero(),
			Method: miner.Methods.ChangeWorkerAddress,
			Nonce:  a.Nonce,
			Params: sp,
		}, nil, types.EmptyTSK)
		if err != nil {
			fmt.Printf("评估消息的gas费用失败， err:%v\n", err)
			return xerrors.Errorf("GasEstimateMessageGas error: %w", err)
		}

		fmt.Printf("\n%+v\n", msg)

		// 签名
		signMsg, err := signMessage(msg)
		if err != nil {
			return err
		}

		// 推送消息
		cid, err := api.MpoolPush(ctx, signMsg)
		if err != nil {
			fmt.Printf("推送消息上链失败，err:%v\n", err)
			return err
		}

		fmt.Println("Message CID:", cid.String())

		return nil
	},
}
var setOwnerCmd = &cli.Command{
	Name:      "set-owner",
	Usage:     "Set owner address (this command should be invoked twice, first with the old owner as the senderAddress, and then with the new owner)",
	ArgsUsage: "[newOwnerAddress senderAddress]",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "really-do-it",
			Usage: "Actually send transaction performing the action",
			Value: false,
		},
	},
	Action: func(cctx *cli.Context) error {
		if !cctx.Bool("really-do-it") {
			fmt.Println("Pass --really-do-it to actually execute this action")
			return nil
		}

		if cctx.NArg() != 3 {
			return fmt.Errorf("must pass miner id, new owner address and sender address")
		}

		//nodeApi, closer, err := GetStorageMinerAPI(cctx)
		//if err != nil {
		//	return err
		//}
		//defer closer()

		api, acloser, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer acloser()

		ctx := lcli.ReqContext(cctx)

		na, err := address.NewFromString(cctx.Args().Get(1))
		if err != nil {
			return err
		}

		newAddrId, err := api.StateLookupID(ctx, na, types.EmptyTSK)
		if err != nil {
			return err
		}

		fa, err := address.NewFromString(cctx.Args().Get(2))
		if err != nil {
			return err
		}

		fromAddrId, err := api.StateLookupID(ctx, fa, types.EmptyTSK)
		if err != nil {
			return err
		}

		//maddr, err := nodeApi.ActorAddress(ctx)
		maddr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		if fromAddrId != mi.Owner && fromAddrId != newAddrId {
			return xerrors.New("from address must either be the old owner or the new owner")
		}

		sp, err := actors.SerializeParams(&newAddrId)
		if err != nil {
			return xerrors.Errorf("serializing params: %w", err)
		}

		// 获取nonce
		a, err := api.StateGetActor(ctx, mi.Owner, types.EmptyTSK)
		if err != nil {
			fmt.Printf("读取获取owner地址的nonce失败，err:%v\n", err)
			return err
		}

		msg, err := api.GasEstimateMessageGas(ctx, &types.Message{
			From:   fromAddrId,
			To:     maddr,
			Method: miner.Methods.ChangeOwnerAddress,
			Value:  big.Zero(),
			Params: sp,
			Nonce:  a.Nonce,
		}, nil, types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("mpool push: %w", err)
		}

		fmt.Printf("\n%+v\n", msg)

		// 签名
		signMsg, err := signMessage(msg)
		if err != nil {
			return err
		}

		// 推送消息
		cid, err := api.MpoolPush(ctx, signMsg)
		if err != nil {
			fmt.Printf("推送消息上链失败，err:%v\n", err)
			return err
		}

		fmt.Println("Message CID:", cid)

		// wait for it to get mined into a block
		wait, err := api.StateWaitMsg(ctx, cid, build.MessageConfidence)
		if err != nil {
			return err
		}

		// check it executed successfully
		if wait.Receipt.ExitCode != 0 {
			fmt.Println("owner change failed!")
			return err
		}

		fmt.Println("message succeeded!")

		return nil
	},
}

var controlListCmd = &cli.Command{
	Name:      "control-list",
	Usage:     "Get currently set control addresses",
	ArgsUsage: "[minerId]",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "verbose",
		},
		&cli.BoolFlag{
			Name:  "color",
			Value: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		color.NoColor = !cctx.Bool("color")

		//nodeApi, closer, err := GetStorageMinerAPI(cctx)
		//if err != nil {
		//	return err
		//}
		//defer closer()

		api, acloser, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer acloser()

		ctx := lcli.ReqContext(cctx)

		//maddr, err := nodeApi.ActorAddress(ctx)
		maddr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		tw := tablewriter.New(
			tablewriter.Col("name"),
			tablewriter.Col("ID"),
			tablewriter.Col("key"),
			tablewriter.Col("use"),
			tablewriter.Col("balance"),
		)

		//ac, err := nodeApi.ActorAddressConfig(ctx)
		//if err != nil {
		//	return err
		//}

		commit := map[address.Address]struct{}{}
		precommit := map[address.Address]struct{}{}
		terminate := map[address.Address]struct{}{}
		post := map[address.Address]struct{}{}

		for _, ca := range mi.ControlAddresses {
			post[ca] = struct{}{}
		}

		//for _, ca := range ac.PreCommitControl {
		//	ca, err := api.StateLookupID(ctx, ca, types.EmptyTSK)
		//	if err != nil {
		//		return err
		//	}

		//	delete(post, ca)
		//	precommit[ca] = struct{}{}
		//}

		//for _, ca := range ac.CommitControl {
		//	ca, err := api.StateLookupID(ctx, ca, types.EmptyTSK)
		//	if err != nil {
		//		return err
		//	}

		//	delete(post, ca)
		//	commit[ca] = struct{}{}
		//}

		//for _, ca := range ac.TerminateControl {
		//	ca, err := api.StateLookupID(ctx, ca, types.EmptyTSK)
		//	if err != nil {
		//		return err
		//	}

		//	delete(post, ca)
		//	terminate[ca] = struct{}{}
		//}

		printKey := func(name string, a address.Address) {
			b, err := api.WalletBalance(ctx, a)
			if err != nil {
				fmt.Printf("%s\t%s: error getting balance: %s\n", name, a, err)
				return
			}

			k, err := api.StateAccountKey(ctx, a, types.EmptyTSK)
			if err != nil {
				fmt.Printf("%s\t%s: error getting account key: %s\n", name, a, err)
				return
			}

			kstr := k.String()
			if !cctx.Bool("verbose") {
				kstr = kstr[:9] + "..."
			}

			bstr := types.FIL(b).String()
			switch {
			case b.LessThan(types.FromFil(10)):
				bstr = color.RedString(bstr)
			case b.LessThan(types.FromFil(50)):
				bstr = color.YellowString(bstr)
			default:
				bstr = color.GreenString(bstr)
			}

			var uses []string
			if a == mi.Worker {
				uses = append(uses, color.YellowString("other"))
			}
			if _, ok := post[a]; ok {
				uses = append(uses, color.GreenString("post"))
			}
			if _, ok := precommit[a]; ok {
				uses = append(uses, color.CyanString("precommit"))
			}
			if _, ok := commit[a]; ok {
				uses = append(uses, color.BlueString("commit"))
			}
			if _, ok := terminate[a]; ok {
				uses = append(uses, color.YellowString("terminate"))
			}

			tw.Write(map[string]interface{}{
				"name":    name,
				"ID":      a,
				"key":     kstr,
				"use":     strings.Join(uses, " "),
				"balance": bstr,
			})
		}

		printKey("owner", mi.Owner)
		printKey("worker", mi.Worker)
		printKey("newWorker", mi.NewWorker)
		for i, ca := range mi.ControlAddresses {
			printKey(fmt.Sprintf("control-%d", i), ca)
		}

		return tw.Flush(os.Stdout)
	},
}

var proposeChangeWorker = &cli.Command{
	Name:      "propose-change-worker",
	Usage:     "Propose a worker address change",
	ArgsUsage: "[minerId,address]",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "really-do-it",
			Usage: "Actually send transaction performing the action",
			Value: false,
		},
	},
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return fmt.Errorf("must pass address of new worker address")
		}

		api, acloser, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer acloser()

		ctx := lcli.ReqContext(cctx)

		// 目标地址
		na, err := address.NewFromString(cctx.Args().Get(1))
		if err != nil {
			return err
		}

		newAddr, err := api.StateLookupID(ctx, na, types.EmptyTSK)
		if err != nil {
			return err
		}

		// 矿工地址
		//maddr, err := nodeApi.ActorAddress(ctx)
		maddr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		if mi.NewWorker.Empty() {
			if mi.Worker == newAddr {
				return fmt.Errorf("worker address already set to %s", na)
			}
		} else {
			if mi.NewWorker == newAddr {
				return fmt.Errorf("change to worker address %s already pending", na)
			}
		}

		if !cctx.Bool("really-do-it") {
			fmt.Fprintln(cctx.App.Writer, "Pass --really-do-it to actually execute this action")
			return nil
		}

		cwp := &miner2.ChangeWorkerAddressParams{
			NewWorker:       newAddr,
			NewControlAddrs: mi.ControlAddresses,
		}

		sp, err := actors.SerializeParams(cwp)
		if err != nil {
			return xerrors.Errorf("serializing params: %w", err)
		}

		realOwner, err := api.StateAccountKey(ctx, mi.Owner, types.EmptyTSK)
		if err != nil {
			fmt.Printf("%s\t%s: error getting account key: %s\n", maddr, mi.Owner, err)
			return nil
		}

		// 获取nonce
		a, err := api.StateGetActor(ctx, mi.Owner, types.EmptyTSK)
		if err != nil {
			fmt.Printf("读取获取owner地址的nonce失败，err:%v\n", err)
			return err
		}

		msg, err := api.GasEstimateMessageGas(ctx, &types.Message{
			From:   realOwner,
			To:     maddr,
			Method: miner.Methods.ChangeWorkerAddress,
			Value:  big.Zero(),
			Params: sp,
			Nonce:  a.Nonce,
		}, nil, types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("mpool push: %w", err)
		}

		fmt.Printf("\n%+v\n", msg)

		// 签名
		signMsg, err := signMessage(msg)
		if err != nil {
			return err
		}

		// 推送消息
		cid, err := api.MpoolPush(ctx, signMsg)
		if err != nil {
			fmt.Printf("推送消息上链失败，err:%v\n", err)
			return err
		}

		fmt.Fprintln(cctx.App.Writer, "Propose Message CID:", cid)

		// wait for it to get mined into a block
		wait, err := api.StateWaitMsg(ctx, cid, build.MessageConfidence)
		if err != nil {
			return err
		}

		// check it executed successfully
		if wait.Receipt.ExitCode != 0 {
			fmt.Fprintln(cctx.App.Writer, "Propose worker change failed!")
			return err
		}

		mi, err = api.StateMinerInfo(ctx, maddr, wait.TipSet)
		if err != nil {
			return err
		}
		if mi.NewWorker != newAddr {
			return fmt.Errorf("Proposed worker address change not reflected on chain: expected '%s', found '%s'", na, mi.NewWorker)
		}

		fmt.Fprintf(cctx.App.Writer, "Worker key change to %s successfully proposed.\n", na)
		fmt.Fprintf(cctx.App.Writer, "Call 'confirm-change-worker' at or after height %d to complete.\n", mi.WorkerChangeEpoch)

		return nil
	},
}