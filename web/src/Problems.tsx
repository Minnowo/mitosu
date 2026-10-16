// A host's problems as a list, critical ones first (the server sorts).

import type {Problem} from './api';

export function Problems(props: {problems: Problem[]}) {
    if (props.problems.length === 0) {
        return <p class="rounded-md p-3 bg-c-success-container text-c-on-success-container">No problems found.</p>;
    }
    return (
        <ul class="flex flex-col gap-2">
            {props.problems.map((p) => (
                <li
                    class={`rounded-md p-3 ${p.severity === 'critical' ? 'bg-c-error-container text-c-on-error-container' : 'bg-c-tertiary-container text-c-on-tertiary-container'}`}
                    key={p.severity + p.subject + p.message}
                >
                    <strong>{p.subject}</strong>: {p.message}
                </li>
            ))}
        </ul>
    );
}
