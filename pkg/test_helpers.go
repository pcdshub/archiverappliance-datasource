package main

import (
	"encoding/json"
	"testing"
	"time"
)

func MultiReturnHelperParseDuration(result time.Duration, err error) time.Duration {
	return result
}

func MultiReturnHelperParse(result time.Time, err error) time.Time {
	return result
}

func InitString(value string) *string {
	new_string := value
	return &new_string
}

func InitRawMsg(value string) *json.RawMessage {
	new_msg := json.RawMessage(value)
	return &new_msg
}

func InitIntPointer(value int) *int {
	new_int := value
	return &new_int
}

func TimeHelper(minutes int) time.Time {
	// Shortcut for generating consistent timestamps using only a single int
	return time.Date(2021, time.January, 10, 1, minutes, 0, 0, time.UTC)
}

func TimeArrayHelper(start int, end int) []time.Time {
	// Shortcut for generating a slice of consistent timestamps using the [beginning, ending) ints
	counter := start
	var increment int
	if start < end {
		increment = 1
	} else {
		increment = -1
	}
	var response []time.Time
	for counter != end {
		counter = counter + increment
		response = append(response, TimeHelper(counter))
	}
	return response
}

func SingleDataCompareHelper(result []*SingleData, wanted []*SingleData, t *testing.T) {
	// Raise no errors if two []SingleData are identical, raise errors if they are not
	if len(result) != len(wanted) {
		t.Errorf("Input and output SingleData differ in length. Wanted %v, got %v", len(wanted), len(result))
		return
	}
	for udx := range wanted {
		if result[udx].Name != wanted[udx].Name {
			t.Errorf("Input and output SingleData have different Pvs. Wanted %v, got %v", wanted[udx].Name, result[udx].Name)
		}

		switch resultv := result[udx].Values.(type) {
		case *Scalars:
			wantedv := wanted[udx].Values.(*Scalars)
			if len(wantedv.Times) != len(resultv.Times) {
				t.Errorf("Input and output arrays' times differ in length. Wanted %v, got %v", len(wantedv.Times), len(resultv.Times))
				return
			}
			if len(wantedv.Values) != len(resultv.Values) {
				t.Errorf("Input and output arrays' values differ in length. Wanted %v, got %v", len(wantedv.Values), len(resultv.Values))
				return
			}
			for idx := range wantedv.Values {
				if resultv.Times[idx] != wantedv.Times[idx] {
					t.Errorf("Times at index %v do not match, Wanted %v, got %v", idx, wantedv.Times[idx], resultv.Times[idx])
				}
				if resultv.Values[idx] != wantedv.Values[idx] {
					t.Errorf("Values at index %v do not match, Wanted %v, got %v", idx, wantedv.Values[idx], resultv.Values[idx])
				}
			}
		case *Arrays:
			wantedv := wanted[udx].Values.(*Arrays)
			if len(wantedv.Times) != len(resultv.Times) {
				t.Errorf("Input and output arrays' times differ in length. Wanted %v, got %v", len(wantedv.Times), len(resultv.Times))
				return
			}
			if len(wantedv.Values) != len(resultv.Values) {
				t.Errorf("Input and output arrays' values differ in length. Wanted %v, got %v", len(wantedv.Values), len(resultv.Values))
				return
			}
			for idx := range wantedv.Values {
				if resultv.Times[idx] != wantedv.Times[idx] {
					t.Errorf("Times at index %v do not match, Wanted %v, got %v", idx, wantedv.Times[idx], resultv.Times[idx])
				}
				for idy := range wantedv.Values[idx] {
					if resultv.Values[idx][idy] != wantedv.Values[idx][idy] {
						t.Errorf("Values at index %v do not match, Wanted %v, got %v", idx, wantedv.Values[idx][idy], resultv.Values[idx][idy])
					}
				}
			}
		default:
			t.Fatalf("Response Values are invalid")
		}
	}
}
