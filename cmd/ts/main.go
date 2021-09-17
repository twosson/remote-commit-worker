package main

import (
	"fmt"
	"github.com/gogf/gf/os/gtimer"
	"time"
)

func main() {
	interval := time.Second
	gtimer.AddSingleton(interval, func() {
		fmt.Println("aaa1")
		time.Sleep(5 * time.Second)
		fmt.Println("aaa2")
		time.Sleep(5 * time.Second)
		fmt.Println("aaa3")
		time.Sleep(5 * time.Second)
		fmt.Println("aaa4")
		time.Sleep(5 * time.Second)
		fmt.Println("aaa5")
		time.Sleep(5 * time.Second)
	})
	gtimer.Add(interval, func() {
		fmt.Println("bbb1")
		time.Sleep(5 * time.Second)
		fmt.Println("bbb2")
		time.Sleep(5 * time.Second)
		fmt.Println("bbb3")
		time.Sleep(5 * time.Second)
		fmt.Println("bbb4")
		time.Sleep(5 * time.Second)
		fmt.Println("bbb5")
		time.Sleep(5 * time.Second)
	})
	select {}
}
