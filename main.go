package main

import (
	"github.com/bizshuk/img/cmd"
	"github.com/bizshuk/img/config"
)

func main() {
	config.Default()
	cmd.Execute()
}
