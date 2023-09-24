// Package widgets implement several simple widgets that can be used to
// make your Go programs interact with front-end widgets in a Jupyter
// Notebook, using GoNB kernel.
//
// Because most widgets will have many optional parameters, it uses
// the convention of calling the widget to create a "builder" object,
// have optional parameters as method calls, and then call `Done()`
// to actually display and start it.
//
// If you want to implement a new widget, checkout `gonb/gonbui/comms`
// package for the communication functionality, along with tools for
// building widgets.
package widgets

