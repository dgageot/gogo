package taskfile

import (
	"fmt"

	"github.com/ansxuman/go-touchid"
)

func authenticateBiometric() error {
	ok, err := touchid.Auth(touchid.DeviceTypeAny, "gogo needs to access secrets")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("authentication denied")
	}
	return nil
}
