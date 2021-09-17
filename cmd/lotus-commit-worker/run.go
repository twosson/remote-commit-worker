package main

import (
	"encoding/base64"
	"encoding/json"
	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/gogf/gf/frame/g"
	"github.com/urfave/cli/v2"
	"time"
)

var runCmd = &cli.Command{
	Name:  "run",
	Usage: "Start lotus worker",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "gateway",
			Usage: "gateway address and port the worker api will listen on",
			Value: "58.254.182.123:8399",
		},
		&cli.IntFlag{
			Name:  "parallel",
			Usage: "How many tasks are in parallel",
			Value: 2,
		},
	},
	Action: func(cctx *cli.Context) error {
		gateway := cctx.String("gateway")
		commitTaskURL := "http://" + gateway + "/commit/task"
		commitSubmitURL := "http://" + gateway + "/commit/submit"
		parallel := cctx.Int("parallel")

		for i := 0; i < parallel; i++ {
			go func(taskNumber int) {
				for {
					log.Infof("启动 %d 号算法", taskNumber)
					if r, err := g.Client().Get(commitTaskURL); err != nil {
						log.Errorf("获取任务失败: %s", err.Error())
					} else {
						// 开始做任务
						var data JsonResponse
						if err := json.Unmarshal(r.ReadAll(), &data); err != nil {
							log.Errorf("解析JSON失败: %s", err.Error())
						} else {
							var commitProof CommitProof
							commitProof.SectorNumber = data.Data.SectorNumber
							commitProof.MinerNumber = data.Data.MinerNumber
							if data.Code == -1 {
								log.Info("获取任务失败")
							} else {
								params, err := base64.StdEncoding.DecodeString(data.Data.Params)
								if err != nil {
									log.Errorf("解析C1参数错误: %s", err.Error())
									commitProof.Status = "C1_ERROR"
								} else {
									sectorNumber := abi.SectorNumber(data.Data.SectorNumber)
									minerNumber := abi.ActorID(data.Data.MinerNumber)
									proof, err := ffi.SealCommitPhase2(params, sectorNumber, minerNumber)
									if err != nil {
										log.Errorf("时空证明计算错误: %s", err.Error())
										commitProof.Status = "FFI_ERROR"
									} else {
										proofBase64 := base64.StdEncoding.EncodeToString(proof)
										commitProof.Proof = proofBase64
										commitProof.Status = "FIN"
									}
								}

								if r2, err := g.Client().Post(commitSubmitURL, commitProof); err != nil {
									log.Errorf("提交计算结果错误: %s", err.Error())
								} else {
									r2.Close()
								}
							}
						}
						if err := r.Close(); err != nil {
							log.Error(err)
						}
					}
					log.Infof("结束 %d 号算法", taskNumber)
					time.Sleep(5 * time.Second)
				}
			}(i)
			time.Sleep(15 * time.Second)
		}

		select {}
		return nil
	},
}
