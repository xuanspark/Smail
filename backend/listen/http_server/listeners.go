package http_server

// PrepareWebServers 在提交监听任务前同步创建实例，防止停止后才发布新的监听器。
func PrepareWebServers() (httpRun, httpsRun func() error, err error) {
	httpInstance, err := prepareHTTPServer()
	if err != nil {
		return nil, nil, err
	}
	httpsInstance, err := prepareHTTPSServer()
	if err != nil {
		HttpStop()
		return nil, nil, err
	}
	return func() error { return serveHTTPServer(httpInstance) },
		func() error { return serveHTTPSServer(httpsInstance) }, nil
}
