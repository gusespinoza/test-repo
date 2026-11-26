import { DatePipe } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import { Component, computed, DestroyRef, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';

import { AdminBookingPage } from '../bookings/bookings.models';
import { BookingsService } from '../bookings/bookings.service';

@Component({
  selector: 'app-admin-bookings',
  standalone: true,
  imports: [DatePipe],
  templateUrl: './admin-bookings.component.html',
  styleUrl: './admin.css'
})
export class AdminBookingsComponent {
  private readonly bookingsService = inject(BookingsService);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly result = signal<AdminBookingPage | null>(null);
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

    this.bookingsService
      .listAdminBookings(page)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (result) => {
          this.result.set(result);
          this.pageNumber.set(result.page);
          this.loading.set(false);
        },
        error: (error: HttpErrorResponse) => {
          this.error.set(error.error?.error ?? 'No se pudieron cargar las reservas.');
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
