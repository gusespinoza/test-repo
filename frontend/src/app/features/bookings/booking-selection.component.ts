import { HttpErrorResponse } from '@angular/common/http';
import { Component, DestroyRef, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { ActivatedRoute, Router } from '@angular/router';

import { Event as TicketEvent, EventPage } from '../events/events.models';
import { EventsService } from '../events/events.service';
import { BookingsService } from './bookings.service';

@Component({
  selector: 'app-booking-selection',
  standalone: true,
  templateUrl: './booking-selection.component.html',
  styleUrl: './booking-selection.component.css'
})
export class BookingSelectionComponent {
  private readonly eventsService = inject(EventsService);
  private readonly bookingsService = inject(BookingsService);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly events = signal<TicketEvent[]>([]);
  protected readonly eventID = signal('');
  protected readonly quantity = signal(1);
  protected readonly loadingEvents = signal(true);
  protected readonly submitting = signal(false);
  protected readonly error = signal<string | null>(null);
  private idempotencyKey = crypto.randomUUID();

  constructor() {
    this.eventsService
      .listEvents(1, 100)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (page: EventPage) => {
          this.events.set(page.items);
          this.loadingEvents.set(false);
        },
        error: (error: HttpErrorResponse) => {
          this.error.set(error.error?.error ?? 'No se pudieron cargar los eventos.');
          this.loadingEvents.set(false);
        }
      });

    this.route.queryParamMap.pipe(takeUntilDestroyed(this.destroyRef)).subscribe((params) => {
      const eventID = params.get('event_id');
      if (eventID) {
        this.eventID.set(eventID);
      }
    });
  }

  protected changeEvent(event: Event): void {
    if (event.target instanceof HTMLSelectElement) {
      this.eventID.set(event.target.value);
      this.idempotencyKey = crypto.randomUUID();
    }
  }

  protected changeQuantity(event: Event): void {
    if (event.target instanceof HTMLInputElement) {
      this.quantity.set(event.target.valueAsNumber);
      this.idempotencyKey = crypto.randomUUID();
    }
  }

  protected submit(): void {
    const eventID = this.eventID();
    const quantity = this.quantity();
    if (!eventID || !Number.isInteger(quantity) || quantity <= 0) {
      this.error.set('Selecciona un evento e indica una cantidad mayor que cero.');
      return;
    }

    this.submitting.set(true);
    this.error.set(null);
    this.bookingsService
      .createBooking({
        event_id: eventID,
        quantity,
        idempotency_key: this.idempotencyKey
      })
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (order) => {
          void this.router.navigate(['/bookings', order.id]);
        },
        error: (error: HttpErrorResponse) => {
          this.error.set(error.error?.error ?? 'No se pudo crear la reserva.');
          this.submitting.set(false);
        }
      });
  }
}
