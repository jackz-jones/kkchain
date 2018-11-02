//go:generate go run proto.go

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var (
	goPath = os.Getenv("GOPATH")
)

// Notice that generated file will be placed under current directory. 
func main() {
	if len(os.Args) != 2 {
		fmt.Println(os.Args)
		fmt.Printf("Usage: %s absulute_parent_path_of_proto_files\n", os.Args[0])
		return
	}
	if err := generateProtos(os.Args[1]); err != nil {
		fmt.Printf("%+v", err)
	}
}

func generateProtos(dir string) error {

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		// skip vendor directory
		if info.IsDir() && info.Name() == "vendor" {
			return filepath.SkipDir
		}
		
		// find all protobuf files
		if filepath.Ext(path) == ".proto" {
			fmt.Println(path)
			// args
			args := []string{
				"-I=.",
				fmt.Sprintf("-I=%s", filepath.Join(goPath, "src")),
				fmt.Sprintf("-I=%s", filepath.Join(goPath, "src", "github.com", "gogo", "protobuf", "protobuf")),
				fmt.Sprintf("--proto_path=%s", filepath.Join(goPath, "src", "github.com")),
				"--gogofaster_out=Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/duration.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/struct.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/wrappers.proto=github.com/gogo/protobuf/types:.",
				path,
			}
			cmd := exec.Command("protoc", args...)
			err = cmd.Run()
			if err != nil {
				fmt.Println(err)
				return err
			}
		}
		return nil
	})
}