package main

type JsonResponse struct {
	Code    int          `json:"code"` // 错误码((0:成功, 1:失败, >1:错误码))
	Message string       `json:"msg"`  // 提示信息
	Data    CommitParams `json:"data"` // 返回数据(业务接口定义具体数据结构)
}

type CommitParams struct {
	MinerNumber  int    `json:"minerNumber"`
	SectorNumber int    `json:"sectorNumber"`
	Params       string `json:"params"`
}

type CommitProof struct {
	Status       string `json:"status"`
	MinerNumber  int    `json:"minerNumber"`
	SectorNumber int    `json:"sectorNumber"`
	Proof        string `json:"proof"`
}
