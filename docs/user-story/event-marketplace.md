# User Story for Event Marketplace

## Purpose
- APP can publish their public events to another APP, so that another APP can trigger relevant workflows based on the task. (e.g. The A APP do some upgrading and B APP registered the A APP's event can do sanity checking to make sure there is no impact after the upgrading)
- APP can automatically trigger a workflow according to the required events so that the action can make quickly.
- All the upstream and downstream can be traced. So upstream APP can know which APP subscribe their events and the workflow status of the consumer APPs.
- The downstream APP can know their subscribed event status (include history, current status). So they can know the event health.

## Register an event to event marketplace
- A APP developer wants to register an event which will be sent when the APP makes changes
- The A developer fills the event register form and request the registration within the form via HTTP request or UI
- The marketplace platform gets the request and checks that all fields have no sensitive information
- event is registered into the marketplace

## Browse all public events in marketplace
- The events are labeled to three level: security C, security B, and security A. The label is by APPs, which means APP is categorized as security B, it can publish events with security C and security B.
- APP developer can only see all events with security C in general.
- If APP developers want to see security B's event, they should request the permission in UI.
- When APP developers select an event in marketplace page, they can see the event's details. (input, expected output, the use case, how to use. and how to test it)

## Subscribe an event in marketplace
- The APP developers click the subscribe button for a specific event
- It will trigger an approval workflow.
    - workflow: APP developers need to fill the request form including the purpose -> the request will send to the event publisher -> event publisher review the request and can do more detail checking offline with APP developers -> event publisher click approve -> notify the APP developer that he has the permission to use the event.
- The upstream and downstream relationship of the event will be recorded for tracing.