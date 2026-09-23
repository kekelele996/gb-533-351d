import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { DatePipe, DecimalPipe } from '@angular/common';
import { LucideAngularModule } from 'lucide-angular';
import { CollisionDiff, InterlockDiff, RegressionBaseline } from '../../types/validation-run';

@Component({
  selector: 'app-regression-baseline',
  standalone: true,
  imports: [DatePipe, DecimalPipe, LucideAngularModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (regression(); as regression) {
      <section class="baseline-panel" [class.regression]="regression.has_new_findings">
        <header>
          <div class="title"><lucide-icon [name]="regression.has_new_findings ? 'triangle-alert' : 'scale'" [size]="16" /><div><span>Regression baseline</span><strong>Run #{{ regression.bound.id }} · {{ regression.bound.program_code }} v{{ regression.bound.program_version }}</strong></div></div>
          <span class="gate" [class.blocked]="regression.has_new_findings">
            <lucide-icon [name]="regression.has_new_findings ? 'octagon-x' : 'circle-check'" [size]="14" />
            {{ regression.has_new_findings ? 'New findings block acceptance' : 'No new findings' }}
          </span>
        </header>
        <p class="bound-meta">
          Bound at completion to the latest accepted run for the same robot cell, program code and algorithm
          <code>{{ regression.bound.algorithm_version }}</code>; accepted
          @if (regression.bound.accepted_at) { <time>{{ regression.bound.accepted_at | date:'MMM d, yyyy HH:mm' }}</time> }
          @else { <em>earlier</em> }.
        </p>
        <div class="diff-grid">
          <article class="diff-category">
            <h3><lucide-icon name="scan-line" [size]="14" />Envelope findings</h3>
            <ul class="counts">
              <li class="added"><span class="dot"></span>New<strong>{{ regression.collision_diff.added.length }}</strong></li>
              <li class="removed"><span class="dot"></span>Resolved<strong>{{ regression.collision_diff.removed.length }}</strong></li>
              <li class="persisted"><span class="dot"></span>Still present<strong>{{ regression.collision_diff.persisted.length }}</strong></li>
            </ul>
            @for (item of regression.collision_diff.added; track collisionKey($index, item)) {
              <div class="finding added"><span class="tag">NEW</span><div><strong>{{ item.zone_name }}</strong><em>{{ item.zone_type }} · segment {{ item.segment_index }} · {{ item.first_time_ms | number:'1.0-0' }} ms</em><p>{{ item.evidence }}</p></div></div>
            }
            @for (item of regression.collision_diff.removed; track collisionKey($index, item)) {
              <div class="finding removed"><span class="tag">GONE</span><div><strong>{{ item.zone_name }}</strong><em>{{ item.zone_type }} · segment {{ item.segment_index }} · {{ item.first_time_ms | number:'1.0-0' }} ms</em><p>{{ item.evidence }}</p></div></div>
            }
            @for (item of regression.collision_diff.persisted; track collisionKey($index, item)) {
              <div class="finding persisted"><span class="tag">SAME</span><div><strong>{{ item.zone_name }}</strong><em>{{ item.zone_type }} · segment {{ item.segment_index }} · {{ item.first_time_ms | number:'1.0-0' }} ms</em><p>{{ item.evidence }}</p></div></div>
            }
            @if (totalCollisions() === 0) { <p class="empty">No envelope findings on either side of the baseline.</p> }
          </article>
          <article class="diff-category">
            <h3><lucide-icon name="git-branch" [size]="14" />Interlock findings</h3>
            <ul class="counts">
              <li class="added"><span class="dot"></span>New<strong>{{ regression.interlock_diff.added.length }}</strong></li>
              <li class="removed"><span class="dot"></span>Resolved<strong>{{ regression.interlock_diff.removed.length }}</strong></li>
              <li class="persisted"><span class="dot"></span>Still present<strong>{{ regression.interlock_diff.persisted.length }}</strong></li>
            </ul>
            @for (item of regression.interlock_diff.added; track interlockKey($index, item)) {
              <div class="finding added"><span class="tag">NEW</span><div><strong>{{ item.code }}</strong><em>{{ item.event }}@if (item.depends_on) { → {{ item.depends_on }} }</em><p>{{ item.evidence }}</p>@if (item.path?.length) { <code>{{ item.path!.join(' → ') }}</code> }</div></div>
            }
            @for (item of regression.interlock_diff.removed; track interlockKey($index, item)) {
              <div class="finding removed"><span class="tag">GONE</span><div><strong>{{ item.code }}</strong><em>{{ item.event }}@if (item.depends_on) { → {{ item.depends_on }} }</em><p>{{ item.evidence }}</p>@if (item.path?.length) { <code>{{ item.path!.join(' → ') }}</code> }</div></div>
            }
            @for (item of regression.interlock_diff.persisted; track interlockKey($index, item)) {
              <div class="finding persisted"><span class="tag">SAME</span><div><strong>{{ item.code }}</strong><em>{{ item.event }}@if (item.depends_on) { → {{ item.depends_on }} }</em><p>{{ item.evidence }}</p>@if (item.path?.length) { <code>{{ item.path!.join(' → ') }}</code> }</div></div>
            }
            @if (totalInterlocks() === 0) { <p class="empty">No interlock findings on either side of the baseline.</p> }
          </article>
        </div>
      </section>
    }
  `,
  styles: [`
    .baseline-panel{margin:14px;border:1px solid #c6cfcc;border-radius:4px;overflow:hidden;background:#f4f7f3}
    .baseline-panel.regression{border-color:#d79c73;background:#fdf6ee}
    .baseline-panel>header{display:flex;align-items:center;justify-content:space-between;gap:10px;padding:10px 13px;background:#e6ebe8;border-bottom:1px solid #c6cfcc}
    .baseline-panel.regression>header{background:#f6e7d4;border-bottom-color:#dfbd95}
    .title{display:flex;align-items:center;gap:9px}.title span{display:block;font-size:9px;text-transform:uppercase;color:#6b777a;letter-spacing:.06em}.title strong{display:block;font-size:12px}
    .gate{display:inline-flex;align-items:center;gap:5px;padding:4px 9px;border-radius:3px;font-size:10px;font-weight:700;text-transform:uppercase;letter-spacing:.04em;color:#31704f;background:#e2eee6;border:1px solid #a9cbb6}
    .gate.blocked{color:#8a3a1f;background:#f3d9c8;border-color:#d79c73}
    .bound-meta{margin:0;padding:9px 13px;font-size:10px;color:#5c696d;line-height:1.5;border-bottom:1px solid #dde3df}
    .bound-meta code{padding:1px 5px;background:#e8ece9;border-radius:2px;font-size:9px}
    .diff-grid{display:grid;grid-template-columns:1fr 1fr}
    .diff-category{min-width:0;padding:10px 13px}.diff-category:first-child{border-right:1px solid #d7dedb}
    .diff-category h3{display:flex;align-items:center;gap:6px;margin:0 0 8px;font-size:11px;text-transform:uppercase;color:#465257}
    .counts{display:flex;gap:6px;list-style:none;margin:0 0 9px;padding:0}
    .counts li{display:flex;align-items:center;gap:5px;flex:1;padding:5px 7px;border-radius:3px;background:#fff;border:1px solid #d8dfdb;font-size:9px;text-transform:uppercase;color:#687578}
    .counts strong{margin-left:auto;font-size:13px;color:#39464a}
    .counts .dot{width:7px;height:7px;border-radius:50%}
    .counts .added .dot,.tag{background:#b4552e}.counts .removed .dot{background:#31704f}.counts .persisted .dot{background:#8a8f84}
    .finding{display:flex;gap:8px;padding:8px 0;border-top:1px solid #e2e6e4}
    .finding .tag{flex:none;height:fit-content;padding:2px 5px;border-radius:2px;color:#fff;font-size:8px;font-weight:800;letter-spacing:.05em}
    .finding.removed .tag{background:#31704f}.finding.persisted .tag{background:#8a8f84}
    .finding strong{display:block;font-size:11px}.finding em{display:block;font-style:normal;font-size:9px;color:#6a777a;margin-top:1px}
    .finding p{margin:4px 0 0;font-size:10px;color:#465257;line-height:1.45}.finding code{display:block;margin-top:4px;padding:4px 6px;background:#eceeea;font-size:9px;word-break:break-all}
    .empty{margin:2px 0;font-size:10px;color:#687578}
    @media(max-width:720px){.diff-grid{grid-template-columns:1fr}.diff-category:first-child{border-right:0;border-bottom:1px solid #d7dedb}}
  `],
})
export class RegressionBaselineComponent {
  readonly regression = input<RegressionBaseline | null>(null);

  readonly totalCollisions = computed(() => {
    const diff = this.regression()?.collision_diff;
    return diff ? diff.added.length + diff.removed.length + diff.persisted.length : 0;
  });
  readonly totalInterlocks = computed(() => {
    const diff = this.regression()?.interlock_diff;
    return diff ? diff.added.length + diff.removed.length + diff.persisted.length : 0;
  });

  collisionKey(index: number, item: CollisionDiff): string {
    return `${index}-${item.segment_index}-${item.zone_id}`;
  }
  interlockKey(index: number, item: InterlockDiff): string {
    return `${index}-${item.code}-${item.event}-${item.depends_on ?? ''}-${(item.path ?? []).join('>')}`;
  }
}
