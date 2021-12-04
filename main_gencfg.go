/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 Kusakabe Si. All Rights Reserved.
 */

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

var gencfg_reader *bufio.Reader

func readFLn(promptF string, checkFn func(string) error, defaultAns func() string, args ...interface{}) string {
	defaultans := defaultAns()
	if defaultans != "" {
		fmt.Printf(promptF+"("+defaultans+") :", args...)
	}
	fmt.Printf(promptF+" :", args...)
	text, err := gencfg_reader.ReadString('\n')
	if err != nil {
		panic(err)
	}
	text = strings.Replace(text, "\n", "", -1)
	if err := checkFn(text); err != nil {
		fmt.Println(err)
		return readFLn(promptF, checkFn, defaultAns, args...)
	}
	if text == "" {
		text = defaultans
	}
	return text
}

func noCheck(string) error {
	return nil
}

func genSuperCfg() error {
	gencfg_reader = bufio.NewReader(os.Stdin)
	/*
		noCheck := func(string) error {
			return nil
		}

		noDefault := func() string {
			return ""
		}

		NetworkName := readFLn("Network name", func(s string) (err error) {
			if len(s) > 12 {
				return fmt.Errorf("Name too long")
			}
			return
		}, func() string { return "MyEgNet" })
		MacPrefix := readFLn("MacAddress Prefix", func(s string) (err error) {
			_, err = tap.GetMacAddr(s, 0)
			return
		}, func() string {
			pbyte := mtypes.RandomBytes(4, []byte{0xaa, 0xbb, 0xcc, 0xdd})
			pbyte[0] &^= 0b00000001
			pbyte[0] |= 0b00000010
			return fmt.Sprintf("%X:%X:%X:%X", pbyte[0], pbyte[1], pbyte[2], pbyte[3])
		})
		IPv4Block := readFLn("MacAddress Prefix", func(s string) error {
			_, _, err := tap.GetIP(4, s, 0)
			return err
		}, noDefault)
		IPv6Block := readFLn("MacAddress Prefix", func(s string) error {
			_, _, err := tap.GetIP(6, s, 0)
			return err
		}, noDefault)
	*/
	return nil
}
