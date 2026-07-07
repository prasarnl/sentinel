package scripts

import _ "embed"

//go:embed install.sh
var InstallSh []byte

//go:embed install.ps1
var InstallPs1 []byte
