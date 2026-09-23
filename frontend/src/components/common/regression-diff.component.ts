import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { LucideAngularModule } from 'lucide-angular';
import { BaselineSummary, FindingIdentity, RegressionDiff } from '../../types/validation-run';

@Component({
  selector: 'app-regression-diff',
  standalone: true,
  imports: [LucideAngularModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="regression" [class.blocked]="diff().has_new_findings && baseline()">
      <header>
        <div><lucide-icon name="git-compare" [size]="16" /><span>Regression baseline</span></div>
        @if (baseline(); as baseline) {
          <a class="baseline-ref">Run #{{ baseline.id }} · {{ baseline.program_code }} v{{ baseline.program_version }} · attempt {{ baseline.attempt }}</a>
        } @else {
          <a class="baseline-ref none">No prior accepted run</a>
        }
      </header>
      @if (diff().has_new_findings && baseline()) {
        <p class="blocked-note"><lucide-icon name="octagon-alert" [size]="15" />New findings are absent from the baseline: review approval cannot be accepted until they are resolved or the baseline changes.</p>
      } @else if (diff().has_new_findings) {
        <p class="first-note"><lucide-icon name="flag" [size]="15" />No accepted predecessor exists yet. Accepting this run establishes the first regression baseline; its findings are recorded rather than treated as regressions.</p>
      } @else {
        <p class="clean-note"><lucide-icon name="circle-check" [size]="15" />No new findings relative to the bound baseline.</p>
      }
      @for (group of groups(); track group.key) {
        <div class="diff-group">
          <h4><lucide-icon [name]="group.key === 'collision' ? 'scan-line' : 'git-branch'" [size]="14" />{{ group.title }}</h4>
          <div class="buckets">
            <div class="bucket new"><span>New</span><strong>{{ group.bucket.new.length }}</strong></div>
            <div class="bucket gone"><span>Gone</span><strong>{{ group.bucket.gone.length }}</strong></div>
            <div class="bucket persisted"><span>Persisted</span><strong>{{ group.bucket.persisted.length }}</strong></div>
          </div>
          @for (item of group.bucket.new; track item.key) {
            <article class="finding new"><span class="tag">new</span><div class="finding-head"><strong>{{ label(item) }}</strong>@if (item.segment !== undefined && item.segment !== null) { <small>segment {{ item.segment }}</small> }</div><p>{{ item.evidence }}</p></article>
          }
          @for (item of group.bucket.gone; track item.key) {
            <article class="finding gone"><span class="tag">gone</span><div class="finding-head"><strong>{{ label(item) }}</strong>@if (item.segment !== undefined && item.segment !== null) { <small>segment {{ item.segment }}</small> }</div><p>{{ item.evidence }}</p></article>
          }
          @for (item of group.bucket.persisted; track item.key) {
            <article class="finding persisted"><span class="tag">persisted</span><div class="finding-head"><strong>{{ label(item) }}</strong>@if (item.segment !== undefined && item.segment !== null) { <small>segment {{ item.segment }}</small> }</div><p>{{ item.evidence }}</p></article>
          }
          @if (!group.bucket.new.length && !group.bucket.gone.length && !group.bucket.persisted.length) {
            <p class="empty-finding">No findings in this run or its baseline.</p>
          }
        </div>
      }
    </section>
  `,
  styles: [`
    .regression{border:1px solid #c1cbc7;border-radius:4px;background:#f4f7f4;margin:14px;overflow:hidden}.regression.blocked{border-color:#d29b95;background:#fbf3f1}
    .regression>header{display:flex;align-items:center;justify-content:space-between;gap:10px;padding:10px 13px;background:#e6ebe8;border-bottom:1px solid #c6cfcc}
    .regression>header div{display:flex;align-items:center;gap:7px;font-size:11px;font-weight:800;text-transform:uppercase;color:#3e4b50}
    .baseline-ref{font-size:10px;color:#35535f;font-weight:650}.baseline-ref.none{color:#7a8588;font-weight:500}
    .blocked-note,.clean-note,.first-note{display:flex;align-items:flex-start;gap:7px;margin:0;padding:9px 13px;font-size:10.5px;line-height:1.45;border-bottom:1px solid #d7dedb}
    .blocked-note{color:#842e28;background:#f6e4e0}.clean-note{color:#2a6347;background:#e7f2eb}.first-note{color:#72510b;background:#fff7dc}
    .diff-group{padding:10px 13px;border-bottom:1px solid #dde4e1}.diff-group:last-child{border-bottom:0}.diff-group h4{display:flex;align-items:center;gap:6px;margin:0 0 8px;font-size:11px;text-transform:uppercase;color:#5a676b}
    .buckets{display:grid;grid-template-columns:repeat(3,1fr);gap:7px;margin-bottom:9px}.bucket{display:grid;gap:1px;padding:7px 9px;border-radius:3px;border:1px solid}.bucket span{font-size:8.5px;text-transform:uppercase;letter-spacing:.04em}.bucket strong{font-size:16px;font-variant-numeric:tabular-nums}
    .bucket.new{background:#faece9;border-color:#dea49e;color:#8b2c27}.bucket.gone{background:#e9f3ec;border-color:#a5cbb3;color:#27603f}.bucket.persisted{background:#f0f2f0;border-color:#c5ceca;color:#4d5a5e}
    .finding{position:relative;padding:7px 8px 7px 64px;margin-bottom:5px;border:1px solid #e0e5e3;border-radius:3px;background:#fafbfa}.finding:last-of-type{margin-bottom:0}.finding.new{border-color:#e3c0bc;background:#fdf5f3}.finding.gone{border-color:#c3dccb;background:#f2f8f4}.finding-head{display:flex;align-items:baseline;gap:8px}.finding-head strong{font-size:11px;color:#313d41;text-transform:capitalize}.finding-head small{font-size:9px;color:#7a8588;font-variant-numeric:tabular-nums}.finding p{margin:3px 0 0;font-size:10px;line-height:1.45;color:#546165}
    .tag{position:absolute;left:8px;top:7px;padding:2px 6px;border-radius:3px;font-size:8px;font-weight:800;text-transform:uppercase;letter-spacing:.05em}.finding.new .tag{background:#a7342c;color:#fff}.finding.gone .tag{background:#31704f;color:#fff}.finding.persisted .tag{background:#707d81;color:#fff}
    .empty-finding{margin:0;font-size:10px;color:#869093}
    @media(max-width:600px){.buckets{grid-template-columns:repeat(3,1fr)}.finding{padding-left:8px;padding-top:24px}}
  `],
})
export class RegressionDiffComponent {
  readonly diff = input.required<RegressionDiff>();
  readonly baseline = input<BaselineSummary | null | undefined>(null);
  readonly groups = computed(() => [
    { key: 'collision', title: 'Collision findings', bucket: this.diff().collision },
    { key: 'interlock', title: 'Interlock findings', bucket: this.diff().interlock },
  ]);

  label(item: FindingIdentity): string {
    if (item.category === 'collision') return item.zone_name ? `${item.code} · ${item.zone_name}` : item.code;
    return item.event ? `${item.code} · ${item.event}` : item.code;
  }
}
