package legacy

// CrashAfterSchedules makes Run stop right after the schedule step, as a killed process would.
func CrashAfterSchedules(f func() bool) { crashAfterSchedules = f }

// FailUndo makes the removal of the scheduler records fail.
func FailUndo(f func() error) { undoFault = f }
