package goproxy

type CheckProxyResponse struct {
	Status   string `json:"status"`
	Query    string `json:"query"`
	Country  string `json:"country"`
	Timezone string `json:"timezone"`
	Lat      string `json:"lat"`
	Lon      string `json:"lon"`
	Isp      string `json:"isp"`
	City     string `json:"city"`
	Region   string `json:"region"`
}

type CheckValidIpResponse struct {
	HadBlacklist  bool        `json:"hadBlacklist"`
	IpRecord      interface{} `json:"ipRecord"`
	IsBlacklisted bool        `json:"isBlacklisted"`
	Message       string      `json:"message"`
	Success       bool        `json:"success"`
}

type CheckProxyV4WorkerResponse struct {
	Success bool `json:"success"`
	Data    struct {
		IPServer string `json:"ipServer"`
		IPRemote string `json:"ipRemote"`
	} `json:"data"`
}

type ProxyInfo struct {
	Protocol string
	Address  string
	Username string
	Password string
}

type CheckProxyState struct {
	Proxy ProxyInfo
	Info  CheckProxyResponse
}
