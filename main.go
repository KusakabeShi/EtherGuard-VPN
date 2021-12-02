//go:build !windows
// +build !windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"runtime"
	"syscall"

	"github.com/KusakabeSi/EtherGuard-VPN/ipc"
	"github.com/KusakabeSi/EtherGuard-VPN/path"
	"github.com/KusakabeSi/EtherGuard-VPN/tap"
	yaml "gopkg.in/yaml.v2"
)

const (
	ExitSetupSuccess = 0
	ExitSetupFailed  = 1
)

const (
	ENV_EG_UAPI_FD  = "EG_UAPI_FD"
	ENV_EG_UAPI_DIR = "EG_UAPI_DIR"
)

func printUsage() {
	fmt.Printf("Usage: %s -s/c CONFIG-PATH\n", os.Args[0])
}

func readYaml(filePath string, out interface{}) (err error) {
	yamlFile, err := ioutil.ReadFile(filePath)
	if err != nil {
		return
	}
	err = yaml.Unmarshal(yamlFile, out)
	return
}

var (
	tconfig      = flag.String("config", "", "Config path for the interface.")
	mode         = flag.String("mode", "", "Running mode. [super|edge|solve]")
	printExample = flag.Bool("example", false, "Print example config")
	bind         = flag.String("bind", "linux", "UDP socket bind mode. [linux|std]\nYou may need std mode if you want to run Etherguard under WSL.")
	nouapi       = flag.Bool("no-uapi", false, "Disable UAPI\nWith UAPI, you can check etherguard status by \"wg\" command")
	version      = flag.Bool("version", false, "Show version")
	help         = flag.Bool("help", false, "Show this help")
)

func main() {
	flag.Parse()
	if *version == true {
		fmt.Printf("etherguard-go %s\n%s-%s\n%s\n\nA full mesh layer 2 VPN powered by Floyd Warshall algorithm.\nInformation available at https://github.com/KusakabeSi/EtherGuard-VPN.\nCopyright (C) Kusakabe Si <si@kskb.eu.org>.\n", Version, runtime.GOOS, runtime.GOARCH, tap.VPP_SUPPORT)
		return
	}
	if *help == true {
		flag.Usage()
		return
	}

	uapiDir := os.Getenv(ENV_EG_UAPI_DIR)
	if uapiDir != "" {
		ipc.SetsocketDirectory(uapiDir)
	}

	var err error
	switch *mode {
	case "edge":
		err = Edge(*tconfig, !*nouapi, *printExample, *bind)
	case "super":
		err = Super(*tconfig, !*nouapi, *printExample, *bind)
	case "solve":
		err = path.Solve(*tconfig, *printExample)
	default:
		flag.Usage()
	}
	if err != nil {
		switch err.(type) {
		case syscall.Errno:
			errno, _ := err.(syscall.Errno)
			os.Exit(int(errno))
		default:
			fmt.Fprintf(os.Stderr, "Error :%v\n", err)
			os.Exit(1)
		}
	}
	return
}
