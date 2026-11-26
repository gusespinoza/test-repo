import { DatePipe } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import { Component, computed, DestroyRef, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { ActivatedRoute, Router, RouterLink } from '@angular/router';
import { EMPTY, of, timer } from 'rxjs';
import { catchError, filter, map, switchMap, tap } from 'rxjs/operators';

import { EventsService } from '../events/events.service';
import { Order } from './bookings.models';
import { BookingsService } from './bookings.service';

@Component({
  selector: 'app-booking-checkout',
  standalone: true,
  imports: [DatePipe, RouterLink],
  templateUrl: './booking-checkout.component.html',
  styleUrl: './booking-checkout.component.css'
})
export class BookingCheckoutComponent {
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly bookingsService = inject(BookingsService);
  private readonly eventsService = inject(EventsService);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly booking = signal<Order | null>(null);
  protected readonly loading = signal(true);
  protected readonly busy = signal(false);
  protected readonly paymentVisible = signal(false);
  protected readonly paymentProcessing = signal(false);
  protected readonly paymentSucceeded = signal(false);
  protected readonly error = signal<string | null>(null);
  protected readonly secondsRemaining = signal(0);
  protected readonly countdown = computed(() => {
    const minutes = Math.floor(this.secondsRemaining() / 60);
    const seconds = this.secondsRemaining() % 60;
    return `${minutes}:${seconds.toString().padStart(2, '0')}`;
  });

  constructor() {
    this.route.paramMap
      .pipe(
        map((params) => params.get('id')),
        filter((id): id is string => id !== null),
        tap(() => {
          this.booking.set(null);
          this.loading.set(true);
          this.error.set(null);
          this.paymentVisible.set(false);
          this.paymentProcessing.set(false);
          this.paymentSucceeded.set(false);
        }),
        switchMap((id) =>
          this.bookingsService.getBooking(id).pipe(
            switchMap((booking) => {
              if (booking.status !== 'pending') {
                return of(booking);
              }
              return this.eventsService.getEvent(booking.event_id).pipe(
                switchMap((event) => {
                  if (event.available <= 0) {
                    void this.router.navigate(['/events', event.id], { replaceUrl: true });
                    return EMPTY;
                  }
                  return of(booking);
                })
              );
            }),
            catchError((error: HttpErrorResponse) => {
              this.error.set(error.error?.error ?? 'No se pudo cargar la reserva.');
              this.loading.set(false);
              return EMPTY;
            })
          )
        ),
        takeUntilDestroyed(this.destroyRef)
      )
      .subscribe((booking) => {
        this.booking.set(booking);
        this.loading.set(false);
        this.updateCountdown();
      });

    timer(0, 1000)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(() => this.updateCountdown());
  }

  protected confirm(): void {
    const booking = this.booking();
    if (
      !booking ||
      booking.status !== 'pending' ||
      this.secondsRemaining() === 0 ||
      this.busy()
    ) {
      return;
    }
    this.error.set(null);
    this.paymentVisible.set(true);
  }

  protected pay(event: SubmitEvent): void {
    event.preventDefault();
    const booking = this.booking();
    if (
      !booking ||
      booking.status !== 'pending' ||
      this.secondsRemaining() === 0 ||
      this.busy()
    ) {
      return;
    }

    this.busy.set(true);
    this.paymentProcessing.set(true);
    this.error.set(null);
    timer(2000)
      .pipe(
        switchMap(() => this.bookingsService.confirmBooking(booking.id)),
        takeUntilDestroyed(this.destroyRef)
      )
      .subscribe({
        next: (confirmedBooking) => {
          this.booking.set(confirmedBooking);
          this.paymentSucceeded.set(true);
          this.paymentProcessing.set(false);
          this.busy.set(false);
          this.updateCountdown();
        },
        error: (error: HttpErrorResponse) => {
          this.error.set(error.error?.error ?? 'No se pudo confirmar la reserva.');
          this.paymentProcessing.set(false);
          this.busy.set(false);
        }
      });
  }

  protected cancel(): void {
    const booking = this.booking();
    if (!booking || booking.status !== 'pending') {
      return;
    }
    this.runAction(this.bookingsService.cancelBooking(booking.id));
  }

  private runAction(request: ReturnType<BookingsService['confirmBooking']>): void {
    this.busy.set(true);
    this.error.set(null);
    request.pipe(takeUntilDestroyed(this.destroyRef)).subscribe({
      next: (booking) => {
        this.booking.set(booking);
        this.busy.set(false);
        this.updateCountdown();
      },
      error: (error: HttpErrorResponse) => {
        this.error.set(error.error?.error ?? 'No se pudo actualizar la reserva.');
        this.busy.set(false);
      }
    });
  }

  private updateCountdown(): void {
    const booking = this.booking();
    if (!booking || booking.status !== 'pending') {
      this.secondsRemaining.set(0);
      return;
    }
    this.secondsRemaining.set(Math.max(0, Math.ceil((Date.parse(booking.expires_at) - Date.now()) / 1000)));
  }
}
