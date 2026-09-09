// Diagnostic only: exercises pure functions, never contacts a server or an agent.
// Exit 1 means the product invariants below are still violated.
import { readFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';

const root = fileURLToPath(new URL('../../', import.meta.url));
const require = createRequire(resolve(root, 'packages/core/package.json'));
const ts = require('typescript');
function evaluate(source) {
  const module = { exports: {} };
  const { outputText } = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  });
  new Function('require', 'module', 'exports', outputText)(require, module, module.exports);
  return module.exports;
}

const revision = '5b68cc711';
const people = evaluate(execFileSync('git', ['show', `${revision}:packages/core/org/people.ts`], { cwd: root, encoding: 'utf8' }));
const editor = evaluate(readFileSync(resolve(root, 'packages/core/org/editor.ts'), 'utf8'));
const unit = (id, members = []) => ({ id, name: id, owner_id: 'human', members, roles: [], autonomy: 'draft', excludes: ['external_effects'], allow: [], deny: [], escalation_quota_per_day: 5 });
const definition = (units, edges = []) => ({ units, edges, rules: [], committees: [], market: { price_cap_usd_ticks: 0, offers_per_agent_per_day: 5, min_offers: 2 } });
const findings = [];
function check(name, condition, observed) { findings.push({ name, result: condition ? 'PASS' : 'FAIL', observed }); }

const original = definition([unit('support', [{ type: 'member', id: 'human' }, { type: 'agent', id: 'manager' }])]);
const selectedManager = people.orgPeople(original).find(p => p.id === 'manager');
const added = people.orgAddTeammate(original, selectedManager.key, { type: 'agent', id: 'new-agent' }, 'Analyst');
const actualManager = people.orgPeople(added).find(p => p.id === 'new-agent').reportsTo;
check('Installed: adding under a selected person respects that manager', actualManager === selectedManager.key, { requested: selectedManager.key, actual: actualManager });

const noManager = people.orgAddTeammate(original, null, { type: 'agent', id: 'new-agent' }, 'Analyst');
check('Installed: adding an agent alone does not implicitly create a team', noManager.units.length === original.units.length, { before: original.units.length, after: noManager.units.length, implicitUnit: noManager.units.at(-1).name, owner: noManager.units.at(-1).owner_id ?? null });

const twoTeams = definition([unit('a'), unit('b')]);
twoTeams.committees = [{ decision_type: 'consent', unit_ids: ['a', 'b'], quorum: 2, max_rounds: 1 }];
const removed = editor.removeOrgUnit(twoTeams, 'b');
check('New build: removing a team does not silently lower approval quorum', removed.committees[0]?.quorum === 2, { before: 2, after: removed.committees[0]?.quorum });

const connected = { ...twoTeams, edges: [{ from: 'b', to: 'a', kind: 'reports_to' }] };
const summary = editor.orgDefinitionChanges(twoTeams, connected);
check('New build: revision comparison signals a relationship-only change', summary.length > 0, { visibleUnitChanges: summary.length, changedEdges: connected.edges });

const invalid = editor.parseEditableOrgDefinition(JSON.stringify(definition([{ ...unit('a'), name: '' }])));
check('New build: editor validation rejects an unnamed team before save', 'error' in invalid, { rejected: 'error' in invalid });

const matrix = definition([unit('a'), unit('b'), unit('c')], [{ from: 'b', to: 'a', kind: 'reports_to' }, { from: 'c', to: 'a', kind: 'reports_to' }, { from: 'c', to: 'b', kind: 'reports_to' }]);
const before = editor.orgLayout(matrix).nodes.find(n => n.unit.id === 'c').y;
const after = editor.orgLayout({ ...matrix, edges: [...matrix.edges].reverse() }).nodes.find(n => n.unit.id === 'c').y;
check('New build: matrix depth is independent of edge ordering', before === after, { before, after });

console.log(JSON.stringify({ installedSourceRevision: revision, findings }, null, 2));
process.exitCode = findings.some(f => f.result === 'FAIL') ? 1 : 0;
