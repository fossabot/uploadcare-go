// Package codec holds encoding decoding reading writing stuff
package codec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/uploadcare/uploadcare-go/internal/config"
	"github.com/uploadcare/uploadcare-go/ucare"
)

// Raw represents raw bytes data
type Raw []byte

// UnmarshalJSON implements json.Unmarshaler
func (v *Raw) UnmarshalJSON(data []byte) error {
	*v = Raw(data)
	return nil
}

// NextRawResulter abstracts reading raw results from paginated api response
type NextRawResulter interface {
	Next() bool
	ReadRawResult() (Raw, error)
}

// ResultBuf implements NextRawResulter
type ResultBuf struct {
	Ctx       context.Context
	ReqMethod string
	Client    ucare.Client

	sync.Mutex         // guards everything below
	NextPage   *string `json:"next"`
	Vals       []Raw   `json:"results"`
	at         int     // index to read from the Vals
}

// Next indicates if there is a result to read
func (b *ResultBuf) Next() bool {
	b.Lock()
	defer b.Unlock()
	return !(b.at >= len(b.Vals) && b.NextPage == nil)
}

// ErrEndOfResults denotes absence of results
var ErrEndOfResults = errors.New("No results are left to read")

// ReadRawResult reads returns next Raw result.
// It makes paginated requests when all results from the current page
// have been read.
func (b *ResultBuf) ReadRawResult() (Raw, error) {
	if !b.Next() {
		return nil, ErrEndOfResults
	}

	b.Lock()
	defer b.Unlock()

	var valsPrev []Raw
	if b.at >= len(b.Vals) && b.NextPage != nil {
		valsPrev, b.Vals = b.Vals, nil

		req, err := b.Client.NewRequest(
			b.Ctx,
			"",
			b.ReqMethod,
			*b.NextPage,
			nil,
		)
		if err != nil {
			return nil, err
		}

		err = b.Client.Do(req, b)
		if err != nil {
			return nil, err
		}

		b.Vals = append(valsPrev, b.Vals...)
	}

	res := b.Vals[b.at]
	b.at++

	return res, nil
}

// EncodeReqQuery encodes data passed as an http.Request query string.
// NOTE: data must be a pointer to a struct type.
func EncodeReqQuery(data interface{}, req *http.Request) error {
	t, v, err := reflectTypeValue(data)
	if err != nil {
		return err
	}

	q := req.URL.Query()
	for i := 0; i < t.NumField(); i++ {
		f := v.Field(i)
		if f.Kind() == reflect.Ptr && f.IsNil() {
			continue
		}

		q.Set(t.Field(i).Tag.Get("form"), fieldValue(f))
	}
	req.URL.RawQuery = q.Encode()
	return nil
}

// EncodeReqFormData encodes data passed as a form data.
// NOTE: data must be a pointer to a struct type.
func EncodeReqFormData(data interface{}) (io.ReadCloser, string, error) {
	t, v, err := reflectTypeValue(data)
	if err != nil {
		return nil, "", err
	}

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)

	if err := writeFormFile(writer, data); err != nil {
		return nil, "", err
	}

	for i := 0; i < t.NumField(); i++ {
		tf, vf := t.Field(i), v.Field(i)

		// file has been already written
		if tf.Name == config.FileFieldName ||
			tf.Name == config.FilenameFieldName {
			continue
		}

		if vf.Kind() == reflect.Struct {
			// packing embedded struct fields
			for i := 0; i < vf.NumField(); i++ {
				writeFormField(
					writer,
					tf.Type.Field(i),
					vf.Field(i),
				)
			}
			continue
		}

		writeFormField(writer, tf, vf)
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}

	return ioutil.NopCloser(body), writer.FormDataContentType(), nil
}

func writeFormFile(
	w *multipart.Writer,
	data interface{},
) error {
	dataType := reflect.TypeOf(data).Elem()

	fileField, ok := dataType.FieldByName(config.FileFieldName)
	if !ok {
		return nil
	}

	dataVal := reflect.ValueOf(data).Elem()

	f, ok := dataVal.
		FieldByName(config.FileFieldName).
		Interface().(io.Reader)
	if !ok {
		return errors.New("File must be of type io.Reader")
	}

	formFieldName := fileField.Tag.Get("form")

	fileName, ok := dataVal.
		FieldByName(config.FilenameFieldName).
		Interface().(string)
	if !ok {
		return errors.New("File name must be of type string")
	}
	if fileName == "" {
		return errors.New("File name can't be empty string")
	}

	part, err := w.CreateFormFile(formFieldName, fileName)
	if err != nil {
		return err
	}

	if _, err = io.Copy(part, f); err != nil {
		return err
	}

	return nil
}

func writeFormField(
	w *multipart.Writer,
	t reflect.StructField,
	f reflect.Value,
) {
	if f.Kind() == reflect.Ptr && f.IsNil() {
		return
	}

	formKey := t.Tag.Get("form")
	if formKey == "" {
		return
	}

	_ = w.WriteField(formKey, fieldValue(f))
}

func fieldValue(v reflect.Value) (val string) {
	switch valc := v.Interface().(type) {
	case string:
		val = valc
	case *string:
		val = ucare.StringVal(valc)
	case *uint64:
		val = strconv.FormatUint(ucare.Uint64Val(valc), 10)
	case *int64:
		val = strconv.FormatInt(ucare.Int64Val(valc), 10)
	case *bool:
		val = fmt.Sprintf("%t", ucare.BoolVal(valc))
	case *time.Time:
		val = valc.Format(config.UCTimeLayout)
	}
	return
}

func reflectTypeValue(d interface{}) (t reflect.Type, v reflect.Value, err error) {
	if reflect.TypeOf(d).Kind() != reflect.Ptr {
		err = errors.New("data is not a pointer type")
		return
	}
	t, v = reflect.TypeOf(d).Elem(), reflect.ValueOf(d).Elem()
	if t.Kind() != reflect.Struct {
		err = errors.New("data is not a struct type")
	}
	return
}
