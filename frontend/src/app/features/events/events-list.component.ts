import { DatePipe } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import { Component, computed, DestroyRef, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { RouterLink } from '@angular/router';

import { EventPage } from './events.models';
import { EventsService } from './events.service';

@Component({
  selector: 'app-events-list',
  standalone: true,
  imports: [DatePipe, RouterLink],
  templateUrl: './events-list.component.html',
  styleUrl: './events-list.component.css'
})
export class EventsListComponent {
  private readonly eventsService = inject(EventsService);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly result = signal<EventPage | null>(null);
  protected readonly pageNumber = signal(1);
  protected readonly loading = signal(false);
  protected readonly error = signal<string | null>(null);
  protected readonly pageCount = computed(() => {
    const result = this.result();
    return result ? Math.max(1, Math.ceil(result.total / result.limit)) : 1;
  });

  constructor() {
    this.loadPage(1);
  }

  protected loadPage(page: number): void {
    this.pageNumber.set(page);
    this.loading.set(true);
    this.error.set(null);
    this.result.set(null);

    this.eventsService
      .listEvents(page)
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
}
