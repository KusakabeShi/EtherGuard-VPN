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
	"github.com/KusakabeSi/EtherGuardVPN/tap"
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
	nouapi       = flag.Bool("no-uapi", false, "Do not use UAPI")
	version      = flag.Bool("version", false, "Show version")
	help         = flag.Bool("help", false, "Show this help")
)

func main() {
	flag.Parse()
	if *version == true {
		fmt.Printf("etherguard-go %s\n%s\n\nA full mesh VPN %s-%s.\nInformation available at https://github.com/KusakabeSi/EtherGuardVPN.\nCopyright (C) Kusakabe Si <si@kskb.eu.org>.\n", Version, tap.VPP_SUPPORT, runtime.GOOS, runtime.GOARCH)
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
	case "solve":
		err = path.Solve(*tconfig, *printExample)
	default:
		flag.Usage()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error :%v\n", err)
		os.Exit(1)
	}
	return
}
