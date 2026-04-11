# GΛIΛ 

GAIA is golang based implementation of Ralph Loop (basically "AI continous delivery" with pushes to current branch).
AI instructions and rules are fed from issues so everything can be controlled remotely via tasks with appropriate labels.

## Installation

1. Download project
2. `make build` (ensure go toolchain is installed)
3. `make install` (it will copy binary to ~/bin/gaia)

## Run

```
export PAT=... # github token 
gaia --god --project qbart/gaia --model sonnet
```

- `--god` - enters skip permission mode (default permissions are auto)
- `--project` - source of github issues
- `--model` - overwrite the default claude model (default opus)

Following lables must be created in repo:
- `docs` (instructions for AI)
- `todo` (tasks to be picked up by claude)
- `doing` (claude picked it up and it is currnently working on it)
- `review` (claude finished and mark the issue for review by us, it will not be picked unless rejected)
- `rejected` (review is rejected and claude will pick it up again and apply feedback from comments)
- `done` (review is approved and we can close the task)
- `brainstorm` (instructions for AI to come up with new tasks when nothing to work on, no brainstorm = no brainstorming)

Tasks are implemented in the following order: `doing`, `rejected`, `todo`.

When starting a task, all `docs` are concatenated into single prompt followed by task name and description.

Happy burning tokens! :fire:

