package aptpackage

import (
	"encoding/json"
	"os"
)

func LoadConfig(path string) (*PackPackage, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    var p PackPackage
    err = json.Unmarshal(data, &p)
    return &p, err
}
