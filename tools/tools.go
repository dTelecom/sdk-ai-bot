//go:build tools

package tools

//go:generate go build -o ../bin/mockgen github.com/golang/mock/mockgen

import (
	_ "github.com/golang/mock/mockgen"
)