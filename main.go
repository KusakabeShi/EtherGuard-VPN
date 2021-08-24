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

	"github.com/KusakabeSi/EtherGuardVPN/ipc"
	"github.com/KusakabeSi/EtherGuardVPN/path"
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
	mode         = flag.String("mode", "edge", "Running mode. [super|edge]")
	printExample = flag.Bool("example", false, "Print example config")
	nouapi       = flag.Bool("no-uapi", false, "Do not use UAPI")
	version      = flag.Bool("version", false, "Show version")
	help         = flag.Bool("help", false, "Show this help")
)

func main() {
	flag.Parse()
	if *version == true {
		fmt.Printf("wireguard-go v%s\n\nUserspace WireGuard daemon for %s-%s.\nInformation available at https://www.wireguard.com.\nCopyright (C) Jason A. Donenfeld <Jason@zx2c4.com>.\n", Version, runtime.GOOS, runtime.GOARCH)
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
		err = Edge(*tconfig, !*nouapi, *printExample)
	case "super":
		err = Super(*tconfig, !*nouapi, *printExample)
	case "path":
		path.Solve()
	default:
		flag.Usage()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error :%v\n", err)
		os.Exit(1)
	}
	return
}
