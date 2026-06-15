package stricttoml

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

func Decode(name, data string, out any) error {
	md, err := toml.Decode(data, out)
	if err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	return rejectUnknown(name, md)
}

func DecodeFile(path string, out any) error {
	md, err := toml.DecodeFile(path, out)
	if err != nil {
		return fmt.Errorf("decode manifest %s: %w", path, err)
	}
	return rejectUnknown(path, md)
}

func rejectUnknown(name string, md toml.MetaData) error {
	undecoded := md.Undecoded()
	if len(undecoded) == 0 {
		return nil
	}
	keys := make([]string, len(undecoded))
	for i, key := range undecoded {
		keys[i] = key.String()
	}
	return fmt.Errorf("%s has unknown field(s): %s", name, strings.Join(keys, ", "))
}
