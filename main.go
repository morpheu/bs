// Copyright 2015 bs authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	bsLog "github.com/tsuru/bs/log"
	"github.com/tsuru/bs/metric"
)

const defaultInterval = 60

var config struct {
	DockerEndpoint         string
	TsuruEndpoint          string
	TsuruToken             string
	AppNameEnvVar          string
	ProcessNameEnvVar      string
	MetricsInterval        time.Duration
	StatusInterval         time.Duration
	SyslogListenAddress    string
	SyslogForwardAddresses []string
}

func loadConfig() {
	config.AppNameEnvVar = "TSURU_APPNAME="
	config.ProcessNameEnvVar = "TSURU_PROCESSNAME="
	config.DockerEndpoint = os.Getenv("DOCKER_ENDPOINT")
	config.TsuruEndpoint = os.Getenv("TSURU_ENDPOINT")
	config.TsuruToken = os.Getenv("TSURU_TOKEN")
	statusInterval := os.Getenv("STATUS_INTERVAL")
	parsedInterval, err := strconv.Atoi(statusInterval)
	if err != nil {
		log.Printf("[WARNING] invalid interval %q. Using the default value of %d seconds", statusInterval, defaultInterval)
		parsedInterval = defaultInterval
	}
	config.StatusInterval = time.Duration(parsedInterval) * time.Second
	metricsInterval := os.Getenv("METRICS_INTERVAL")
	parsedMetricsInterval, err := strconv.Atoi(metricsInterval)
	if err != nil {
		log.Printf("[WARNING] invalid metrics interval %q. Using the default value of %d seconds", metricsInterval, defaultInterval)
		parsedMetricsInterval = defaultInterval
	}
	config.MetricsInterval = time.Duration(parsedMetricsInterval) * time.Second
	config.SyslogListenAddress = os.Getenv("SYSLOG_LISTEN_ADDRESS")
	if forwarders := os.Getenv("SYSLOG_FORWARD_ADDRESSES"); forwarders != "" {
		config.SyslogForwardAddresses = strings.Split(forwarders, ",")
	} else {
		config.SyslogForwardAddresses = nil
	}
}

func statusReporter() (chan<- struct{}, <-chan struct{}) {
	abort := make(chan struct{})
	exit := make(chan struct{})
	go func(abort <-chan struct{}) {
		for {
			select {
			case <-abort:
				close(exit)
				return
			case <-time.After(config.StatusInterval):
				reportStatus()
			}
		}
	}(abort)
	return abort, exit
}

func startSignalHandler(callback func(os.Signal), signals ...os.Signal) {
	sigChan := make(chan os.Signal, 4)
	go func() {
		if signal, ok := <-sigChan; ok {
			callback(signal)
		}
	}()
	signal.Notify(sigChan, signals...)
}

func main() {
	loadConfig()
	lf := bsLog.LogForwarder{
		BindAddress:      config.SyslogListenAddress,
		ForwardAddresses: config.SyslogForwardAddresses,
		DockerEndpoint:   config.DockerEndpoint,
		TsuruEndpoint:    config.TsuruEndpoint,
		TsuruToken:       config.TsuruToken,
	}
	err := lf.Start()
	if err != nil {
		log.Fatalf("Unable to initialize log forwarder: %s\n", err)
	}
	mRunner := metric.NewRunner(config.DockerEndpoint, config.MetricsInterval)
	err = mRunner.Start()
	if err != nil {
		log.Printf("Unable to initialize metrics runner: %s\n", err)
	}
	abortReporter, reporterEnded := statusReporter()
	startSignalHandler(func(signal os.Signal) {
		close(abortReporter)
		mRunner.Stop()
	}, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	<-reporterEnded
}
