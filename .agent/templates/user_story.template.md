# User Story: [Title]

## Problem Statement
[Describe the problem or opportunity that this feature addresses.]

## User Story
**As a** [role]
**I want** [feature/capability]
**So that** [benefit/value]

## UI/UX Behavior
* **Trigger:** (What action starts this?)
* **Pre-conditions:** (What must be true before starting? e.g., User is logged in).
* **Success State:** (What does the user see when it works?)
* **Loading State:** (How do we handle latency?)

## 3. Logical Rules (The "If-This-Then-That")
* **Validation:** (e.g., Input cannot be empty, Date must be future).
* **Data Flow:** (e.g., When button is clicked -> Save to DB -> Refresh List).

## 4. Edge Cases & Errors (Crucial)
* **Empty State:** (What if there is no data?)
* **Error Handling:** (What specific error message do we show if it fails?)
* **Limits:** (Max characters, max file size, etc.)

## 5. Acceptance Criteria
*Strictly use GIVEN-WHEN-THEN format. These will be used for automated testing.*

### Scenario A: Happy Path
* **GIVEN** [Pre-condition: e.g., User is logged in as Admin]
* **AND** [Data State: e.g., There are 5 items in the list]
* **WHEN** [Action: e.g., User clicks "Export PDF"]
* **THEN** [Result: e.g., Download starts within 3 seconds]
* **AND** [Data Result: e.g., An "Exported" log is created in the database]

### Scenario B: Unhappy Path (e.g., Network Failure)
* **GIVEN** [Pre-condition: e.g., User is on the payment screen]
* **WHEN** [Action: e.g., User submits payment]
* **AND** [Context: e.g., The API returns a 500 error]
* **THEN** [Result: e.g., Show toast message "Payment failed, please try again"]
* **AND** [UI State: e.g., The "Pay" button becomes enabled again]