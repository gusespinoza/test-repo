import { DatePipe } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import { Component, computed, DestroyRef, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormsModule, NgForm } from '@angular/forms';

import { AdminEventPage, EventInput } from '../events/events.models';
import { EventsService } from '../events/events.service';

@Component({
  selector: 'app-admin-events',
  standalone: true,
  imports: [DatePipe, FormsModule],
  templateUrl: './admin-events.component.html',
  styleUrl: './admin.css'
})
export class AdminEventsComponent {
  private readonly eventsService = inject(EventsService);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly result = signal<AdminEventPage | null>(null);
  protected readonly pageNumber = signal(1);
  protected readonly pageCount = computed(() => {
    const result = this.result();
    return result ? Math.max(1, Math.ceil(result.total / result.limit)) : 1;
  });
  protected readonly loading = signal(false);
  protected readonly submitting = signal(false);
  protected readonly error = signal<string | null>(null);
  protected readonly success = signal<string | null>(null);
  protected readonly createFormVisible = signal(false);
  protected draft: EventInput = this.emptyDraft();

  constructor() {
    this.loadPage(1);
  }

  protected loadPage(page: number): void {
    this.pageNumber.set(page);
    this.loading.set(true);
    this.error.set(null);
    this.result.set(null);

    this.eventsService
      .listAdminEvents(page)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (result) => {
          this.result.set(result);
          this.pageNumber.set(result.page);
          this.loading.set(false);
        },
        error: (error: HttpErrorResponse) => {
          this.error.set(error.error?.error ?? 'No se pudieron cargar los eventos.');
          this.loading.set(false);
        }
      });
  }

  protected toggleCreateForm(): void {
    this.createFormVisible.update((visible) => !visible);
    this.error.set(null);
    this.success.set(null);
  }

  protected createEvent(form: NgForm): void {
    if (form.invalid) {
      form.control.markAllAsTouched();
      return;
    }

    const startsAt = new Date(this.draft.starts_at);
    if (Number.isNaN(startsAt.getTime())) {
      this.error.set('Indica una fecha válida para el evento.');
      return;
    }

    this.submitting.set(true);
    this.error.set(null);
    this.success.set(null);
    this.eventsService
      .createEvent({ ...this.draft, starts_at: startsAt.toISOString() })
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: () => {
          this.submitting.set(false);
          this.createFormVisible.set(false);
          this.draft = this.emptyDraft();
          form.resetForm(this.draft);
          this.success.set('El evento se creó correctamente.');
          this.loadPage(1);
        },
        error: (error: HttpErrorResponse) => {
          this.error.set(error.error?.error ?? 'No se pudo crear el evento.');
          this.submitting.set(false);
        }
      });
  }

  protected previousPage(): void {
    if (this.pageNumber() > 1) {
      this.loadPage(this.pageNumber() - 1);
    }
  }

  protected nextPage(): void {
    if (this.pageNumber() < this.pageCount()) {
      this.loadPage(this.pageNumber() + 1);
    }
  }

  private emptyDraft(): EventInput {
    return { title: '', description: '', venue: '', starts_at: '', capacity: 1 };
  }
}
