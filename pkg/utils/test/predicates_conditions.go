package test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// Predicates aggregates a list of predicates to be used for matching
// objects in a Kubernetes cluster. It is a generic type that can be used
// with any Kubernetes object type that implements the client.Object interface.
type Predicates[T client.Object] struct {
	t          *testing.T
	client     client.Client
	predicates []func(T) bool
}

// Match performs a match using the previously defined predicates.
func (p Predicates[T]) Match(obj T) func() bool {
	return func() bool {
		nn := client.ObjectKeyFromObject(obj)
		err := p.client.Get(p.t.Context(), nn, obj)
		if err != nil {
			p.t.Errorf("failed to get object %T %v: %v", obj, nn, err)
			return false
		}

		for _, pred := range p.predicates {
			if !pred(obj) {
				return false
			}
		}
		return true
	}
}

// Add adds a new predicate to the list of predicates.
func (p Predicates[T]) Add(
	f func(T) bool,
) Predicates[T] {
	p.predicates = append(p.predicates, f)
	return p
}

// Matcher is an interface that defines a method for matching Kubernetes objects.
type Matcher[T client.Object] interface {
	Match() func(T) bool
}

// AddMatch adds a new matcher to the list of predicates.
func (p Predicates[T]) AddMatch(
	m Matcher[T],
) Predicates[T] {
	p.predicates = append(p.predicates, m.Match())
	return p
}

// ObjectPredicates creates a new instance of Predicates with the provided
// client.Client and a list of predicates. It is a helper function that
// simplifies the creation of Predicates for testing purposes.
func ObjectPredicates[
	T client.Object,
](
	t *testing.T,
	cl client.Client,
	predicates ...func(T) bool,
) Predicates[T] {
	t.Helper()

	return Predicates[T]{
		t:          t,
		client:     cl,
		predicates: predicates,
	}
}

// ObjectConditionsAware is an interface that extends the ConditionsAware
// interface from the k8sutils package. It is used to define objects that
// have conditions associated with them.
type ObjectConditionsAware interface {
	k8sutils.ConditionsAware
	client.Object
}

// MatchCondition is a helper function that creates a new instance of ConditionMatcher.
func MatchCondition[T ObjectConditionsAware](t *testing.T) *ConditionMatcher[T] {
	t.Helper()

	return &ConditionMatcher[T]{
		t: t,
	}
}

// ConditionMatcher is a struct that implements the Matcher interface
// for matching Kubernetes objects based on their conditions.
type ConditionMatcher[T ObjectConditionsAware] struct {
	t     *testing.T
	preds []func(metav1.Condition) bool
}

// Type sets the expected type of the condition to match.
func (cm *ConditionMatcher[T]) Type(typ string) *ConditionMatcher[T] {
	cm.preds = append(cm.preds, func(cond metav1.Condition) bool {
		return cond.Type == typ
	})
	return cm
}

// Status sets the expected status of the condition to match.
func (cm *ConditionMatcher[T]) Status(status metav1.ConditionStatus) *ConditionMatcher[T] {
	cm.preds = append(cm.preds, func(cond metav1.Condition) bool {
		return cond.Status == status
	})
	return cm
}

// Reason sets the expected reason of the condition to match.
func (cm *ConditionMatcher[T]) Reason(reason string) *ConditionMatcher[T] {
	cm.preds = append(cm.preds, func(cond metav1.Condition) bool {
		return cond.Reason == reason
	})
	return cm
}

// Message sets the expected message of the condition to match.
func (cm *ConditionMatcher[T]) Message(msg string) *ConditionMatcher[T] {
	cm.preds = append(cm.preds, func(cond metav1.Condition) bool {
		return cond.Message == msg
	})
	return cm
}

// Predicate returns a function that checks if the object has conditions that
// match all the predicates defined in the ConditionMatcher.
// It is supposed to be used with require's/assert's Eventually function.
func (cm ConditionMatcher[T]) Predicate() func(T) bool {
	return func(obj T) bool {
		conds := obj.GetConditions()
		if len(conds) == 0 {
			return false
		}

	outLoop:
		for _, cond := range conds {
			matched := 0
			for _, pred := range cm.preds {
				if !pred(cond) {
					continue outLoop
				}
				matched++
			}

			if matched == len(cm.preds) {
				cm.t.Logf("matched %T %v conditions", obj, client.ObjectKeyFromObject(obj))
				return true
			}
		}
		cm.t.Logf("failed to match object's conditions: %#+v", conds)
		return false
	}
}
