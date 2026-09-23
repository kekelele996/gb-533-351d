import { inject, Injectable, signal } from '@angular/core';
import { finalize } from 'rxjs';
import { ValidationRunApi } from '../api/validation-run';
import { ValidationRun } from '../types/validation-run';
import { apiErrorMessage } from '../utils/api-error';

@Injectable({ providedIn: 'root' })
export class ValidationRunStore {
  private readonly api = inject(ValidationRunApi);
  readonly items = signal<ValidationRun[]>([]);
  readonly selected = signal<ValidationRun | null>(null);
  readonly loading = signal(false);
  readonly error = signal('');

  load(): void {
    this.loading.set(true);
    this.error.set('');
    this.api.list().pipe(finalize(() => this.loading.set(false))).subscribe({
      next: ({ data }) => {
        const previousID = this.selected()?.id;
        this.items.set(data);
        // Keep the viewed run in sync with the server copy so baseline bindings and
        // diffs stay identical after a refresh or a baseline invalidation elsewhere.
        const refreshed = (previousID ? data.find((item) => item.id === previousID) : undefined) ?? data[0];
        this.selected.set(refreshed ?? null);
      },
      error: (error) => this.error.set(apiErrorMessage(error)),
    });
  }

  choose(run: ValidationRun): void { this.selected.set(run); }
  create(programId: number, retryFailed = false): void {
    const key = `ui-${programId}-${Date.now()}-${crypto.randomUUID()}`;
    this.mutate(this.api.create(programId, key, retryFailed));
  }
  review(run: ValidationRun, note: string): void { this.mutate(this.api.review(run.id, note)); }
  accept(run: ValidationRun, note: string): void { this.mutate(this.api.accept(run.id, note)); }
  void(run: ValidationRun, note: string): void { this.mutate(this.api.void(run.id, note)); }

  private mutate(request: ReturnType<ValidationRunApi['create']>): void {
    this.loading.set(true);
    this.error.set('');
    request.pipe(finalize(() => this.loading.set(false))).subscribe({
      next: ({ data }) => {
        this.items.update((items) => [data, ...items.filter((item) => item.id !== data.id)]);
        this.selected.set(data);
      },
      error: (error) => this.error.set(apiErrorMessage(error)),
    });
  }
}
