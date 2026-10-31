on replaceText(findText, replacementText, sourceText)
	set previousDelimiters to AppleScript's text item delimiters
	set AppleScript's text item delimiters to findText
	set sourceItems to every text item of sourceText
	set AppleScript's text item delimiters to replacementText
	set replacedText to sourceItems as text
	set AppleScript's text item delimiters to previousDelimiters
	return replacedText
end replaceText

on jsonString(sourceValue)
	set escapedText to sourceValue as text
	set escapedText to my replaceText("\\", "\\\\", escapedText)
	set escapedText to my replaceText("\"", "\\\"", escapedText)
	set escapedText to my replaceText(return, "\\n", escapedText)
	set escapedText to my replaceText(linefeed, "\\n", escapedText)
	return "\"" & escapedText & "\""
end jsonString

on joinText(textItems, delimiterText)
	set previousDelimiters to AppleScript's text item delimiters
	set AppleScript's text item delimiters to delimiterText
	set joinedText to textItems as text
	set AppleScript's text item delimiters to previousDelimiters
	return joinedText
end joinText

set listSpecs to {{"today", "Today"}, {"inbox", "Inbox"}, {"anytime", "Anytime"}, {"upcoming", "Upcoming"}, {"someday", "Someday"}}
set listEntries to {}

tell application "Things3"
	repeat with listSpec in listSpecs
		set manifestName to item 1 of listSpec
		set thingsName to item 2 of listSpec
		set taskEntries to {}
		set taskPosition to 0
		set listTasks to to dos of list thingsName

		repeat with currentTask in listTasks
			set taskID to id of currentTask
			set taskStatus to status of currentTask as text
			set taskType to "to do"
			if class of currentTask is project then set taskType to "project"
			set projectID to ""
			set areaID to ""

			try
				set parentProject to project of currentTask
				if parentProject is not missing value then set projectID to id of parentProject
			end try
			try
				set parentArea to area of currentTask
				if parentArea is not missing value then set areaID to id of parentArea
			end try

			set taskJSON to "{\"uuid\":" & my jsonString(taskID) & ",\"type\":" & my jsonString(taskType) & ",\"status\":" & my jsonString(taskStatus) & ",\"position\":" & taskPosition & ",\"project\":" & my jsonString(projectID) & ",\"area\":" & my jsonString(areaID) & "}"
			copy taskJSON to end of taskEntries
			set taskPosition to taskPosition + 1
		end repeat

		set listJSON to my jsonString(manifestName) & ":[" & my joinText(taskEntries, ",") & "]"
		copy listJSON to end of listEntries
	end repeat
end tell

set exportedAt to do shell script "date -u +%Y-%m-%dT%H:%M:%SZ"
return "{\"schemaVersion\":1,\"source\":\"things3-applescript\",\"exportedAt\":" & my jsonString(exportedAt) & ",\"lists\":{" & my joinText(listEntries, ",") & "}}"
