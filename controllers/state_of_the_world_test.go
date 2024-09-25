package controllers

import (
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

func TestGetOldest(t *testing.T) {
	type args struct {
		kuadrants []*kuadrantv1beta1.Kuadrant
	}
	tests := []struct {
		name    string
		args    args
		want    *kuadrantv1beta1.Kuadrant
		wantErr bool
	}{
		{
			name: "oldest is first",
			args: args{
				kuadrants: []*kuadrantv1beta1.Kuadrant{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Expected",
							CreationTimestamp: metav1.Time{
								Time: time.Unix(1, 0),
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "UnExpected",
							CreationTimestamp: metav1.Time{
								Time: time.Unix(2, 0),
							},
						},
					},
				},
			},
			want: &kuadrantv1beta1.Kuadrant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "Expected",
					CreationTimestamp: metav1.Time{
						Time: time.Unix(1, 0),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "oldest is second",
			args: args{
				kuadrants: []*kuadrantv1beta1.Kuadrant{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "UnExpected",
							CreationTimestamp: metav1.Time{
								Time: time.Unix(2, 0),
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Expected",
							CreationTimestamp: metav1.Time{
								Time: time.Unix(1, 0),
							},
						},
					},
				},
			},
			want: &kuadrantv1beta1.Kuadrant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "Expected",
					CreationTimestamp: metav1.Time{
						Time: time.Unix(1, 0),
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "Empty list is passed",
			args:    args{kuadrants: []*kuadrantv1beta1.Kuadrant{}},
			want:    nil,
			wantErr: true,
		},
		{
			name: "List contains nil pointer",
			args: args{
				kuadrants: []*kuadrantv1beta1.Kuadrant{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "UnExpected",
							CreationTimestamp: metav1.Time{
								Time: time.Unix(2, 0),
							},
						},
					},
					nil,
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Expected",
							CreationTimestamp: metav1.Time{
								Time: time.Unix(1, 0),
							},
						},
					},
				},
			},
			want: &kuadrantv1beta1.Kuadrant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "Expected",
					CreationTimestamp: metav1.Time{
						Time: time.Unix(1, 0),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "first object is nil pointer",
			args: args{
				kuadrants: []*kuadrantv1beta1.Kuadrant{
					nil,
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "UnExpected",
							CreationTimestamp: metav1.Time{
								Time: time.Unix(2, 0),
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Expected",
							CreationTimestamp: metav1.Time{
								Time: time.Unix(1, 0),
							},
						},
					},
				},
			},
			want: &kuadrantv1beta1.Kuadrant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "Expected",
					CreationTimestamp: metav1.Time{
						Time: time.Unix(1, 0),
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "List contains all nil pointer",
			args:    args{[]*kuadrantv1beta1.Kuadrant{nil, nil, nil}},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetOldestKuadrant(tt.args.kuadrants)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetOldestKuadrant() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetOldestKuadrant() got = %v, want %v", got, tt.want)
			}
		})
	}
}
