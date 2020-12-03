package record

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"

	"k8s.io/klog"
)

type Interface interface {
	Record(Record) error
}

type FlushInterface interface {
	Interface
	Flush(context.Context) error
}

type Record struct {
	Name     string
	Captured time.Time

	Fingerprint string
	Item        Marshalable
}

type Marshalable interface {
	Marshal(context.Context) ([]byte, error)
	GetExtension() string
}

type JSONMarshaller struct {
	Object interface{}
}

func (m JSONMarshaller) Marshal(_ context.Context) ([]byte, error) {
	return json.Marshal(m.Object)
}

// GetExtension return extension for json marshaller
func (m JSONMarshaller) GetExtension() string {
	return "json"
}

type gatherStatusReport struct {
	Name    string        `json:"name"`
	Elapsed time.Duration `json:"elapsed"`
	Report  int           `json:"report"`
	Errors  []error       `json:"errors"`
}

// Collect is a helper for gathering a large set of records from generic functions.
func Collect(ctx context.Context, recorder Interface, bulkFns ...func() ([]Record, []error)) error {
	var errors []string
	var gatherReport []interface{}
	for _, bulkFn := range bulkFns {
		gatherName := runtime.FuncForPC(reflect.ValueOf(bulkFn).Pointer()).Name()
		klog.V(5).Infof("Gathering %s", gatherName)

		start := time.Now()
		records, errs := bulkFn()
		elapsed := time.Now().Sub(start).Truncate(time.Millisecond)

		klog.V(4).Infof("Gather %s took %s to process %d records", gatherName, elapsed, len(records))
		gatherReport = append(gatherReport, gatherStatusReport{gatherName, elapsed, len(records), errs})

		for _, err := range errs {
			errors = append(errors, err.Error())
		}
		for _, record := range records {
			if err := recorder.Record(record); err != nil {
				errors = append(errors, fmt.Sprintf("unable to record %s: %v", record.Name, err))
				continue
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}

	// Creates the gathering performance report
	if err := recordGatherReport(recorder, gatherReport); err != nil {
		errors = append(errors, fmt.Sprintf("unable to record io status reports: %v", err))
	}

	if len(errors) > 0 {
		sort.Strings(errors)
		errors = uniqueStrings(errors)
		return fmt.Errorf("%s", strings.Join(errors, ", "))
	}
	return nil
}

func recordGatherReport(recorder Interface, report []interface{}) error {
	r := Record{Name: "insights-operator/gathers", Item: JSONMarshaller{Object: report}}
	return recorder.Record(r)
}

func uniqueStrings(arr []string) []string {
	var last int
	for i := 1; i < len(arr); i++ {
		if arr[i] == arr[last] {
			continue
		}
		last++
		if last != i {
			arr[last] = arr[i]
		}
	}
	if last < len(arr) {
		last++
	}
	return arr[:last]
}
