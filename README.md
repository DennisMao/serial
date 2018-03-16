# 串口模块

本模块是一个使用Go语言编写的支持多系统下串口通讯模块，支持正则匹配应答指令，超时退出等功能。具有操作简单易用和多平台支持的特性。原作者[argandas](https://github.com/argandas)，本版本在原作者基础上修复了部分使用中发现的问题，增加独立日志模块。

## 如何安装

在终端下使用

```bash
go get github.com/DennisMao/serial
```

## 程序样例

```go
package main
 
import (
	"time"
	"github.com/DennisMao/serial"
)

func main() {
    sp := serial.New()
    err := sp.Open("COM1", 9600)
    if err != nil {
        panic(err)
    }
    defer sp.Close()
    sp.Println("AT")
    sp.WaitForRegexTimeout("OK.*", time.Second * 10)
}
```

## 非阻塞模式

在本模块默认的阻塞模式下，调用Read()操作，系统会一阻塞在函数内，并等待至少有一个byte数据返回的时候才退出。因此如果上述功能不符合需求，可以使用非阻塞模式。在非阻塞模式下，程序在初始化时候需要加入一个超时参数，当调用Read()操作无数据返回时会自动超时退出。

非阻塞模式设置:
```go
sp := serial.New()
err := sp.Open("COM1", 9600, time.Second * 5) //5s超时设置
```
