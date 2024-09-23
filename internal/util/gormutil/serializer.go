package gormutil

import (
	"context"
	"fmt"
	"reflect"

	"github.com/alex65536/go-chess/chess"
	"github.com/alex65536/go-chess/clock"
	"gorm.io/gorm/schema"
)

type ChessSerializer struct{}

func (ChessSerializer) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, dbValue any) error {
	srcTy := field.FieldType
	noPtrTy := srcTy
	if srcTy.Kind() == reflect.Pointer {
		noPtrTy = srcTy.Elem()
	}
	if noPtrTy != reflect.TypeFor[chess.RawBoard]() &&
		noPtrTy != reflect.TypeFor[clock.Control]() &&
		noPtrTy != reflect.TypeFor[chess.Status]() {
		return fmt.Errorf("bad field value type: %v", srcTy)
	}
	if dbValue == nil {
		field.ReflectValueOf(ctx, dst).Set(reflect.New(field.FieldType).Elem())
		return nil
	}
	var data string
	switch v := dbValue.(type) {
	case []byte:
		data = string(v)
	case string:
		data = v
	default:
		return fmt.Errorf("bad db value type: %T", dbValue)
	}
	val := field.ReflectValueOf(ctx, dst)
	if noPtrTy == reflect.TypeFor[chess.RawBoard]() {
		r, err := chess.RawBoardFromFEN(data)
		if err != nil {
			return fmt.Errorf("raw board from fen: %w", err)
		}
		if srcTy.Kind() == reflect.Pointer {
			val.Set(reflect.ValueOf(&r))
		} else {
			val.Set(reflect.ValueOf(r))
		}
		return nil
	}
	if noPtrTy == reflect.TypeFor[clock.Control]() {
		c, err := clock.ControlFromString(data)
		if err != nil {
			return fmt.Errorf("time control from string: %w", err)
		}
		if srcTy.Kind() == reflect.Pointer {
			val.Set(reflect.ValueOf(&c))
		} else {
			val.Set(reflect.ValueOf(c))
		}
		return nil
	}
	if noPtrTy == reflect.TypeFor[chess.Status]() {
		s, err := chess.StatusFromString(data)
		if err != nil {
			return fmt.Errorf("status from string: %w", err)
		}
		if srcTy.Kind() == reflect.Pointer {
			val.Set(reflect.ValueOf(&s))
		} else {
			val.Set(reflect.ValueOf(s))
		}
		return nil
	}
	panic("must not happen")
}

func (ChessSerializer) Value(ctx context.Context, field *schema.Field, dst reflect.Value, fieldValue any) (any, error) {
	switch v := fieldValue.(type) {
	case chess.RawBoard:
		return v.String(), nil
	case *chess.RawBoard:
		if v == nil {
			return nil, nil
		}
		return v.String(), nil
	case clock.Control:
		return v.String(), nil
	case *clock.Control:
		if v == nil {
			return nil, nil
		}
		return v.String(), nil
	case chess.Status:
		return v.String(), nil
	case *chess.Status:
		if v == nil {
			return nil, nil
		}
		return v.String(), nil
	default:
		return nil, fmt.Errorf("bad value type %T", fieldValue)
	}
}

func init() {
	schema.RegisterSerializer("chess", ChessSerializer{})
}
