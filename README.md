# sinastockgo

基于 Go 的新浪多市场实时行情终端，支持自选列表、搜索和实时更新。

## Windows 编译

先安装 [Go](https://go.dev/dl/)，然后在 PowerShell 中执行：

```powershell
git clone https://github.com/windssmile/sinastockgo.git
cd sinastockgo
go build -trimpath -o sinago.exe .
.\sinago.exe
```

首次编译会自动下载 `go.mod` 中声明的依赖。程序的自选列表和日志保存在：

```text
%AppData%\sinago\
```

## 验证

```powershell
go test ./...
```

当前源码已在 macOS/arm64 上通过全部测试，并成功交叉编译为 Windows/amd64 PE32+ 控制台程序。实际 Windows 终端中的显示、输入和联网行情仍需在 Windows 机器上验证。
