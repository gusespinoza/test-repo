import { HttpErrorResponse } from '@angular/common/http';
import { Component, computed, DestroyRef, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { ActivatedRoute, Router } from '@angular/router';
import { EMPTY, switchMap } from 'rxjs';

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
  protected readonly checkingAvailability = signal(false);
  protected readonly selectedAvailability = signal<number | null>(null);
  protected readonly submitting = signal(false);
  protected readonly error = signal<string | null>(null);
  protected readonly selectedSoldOut = computed(() => {
    const available = this.selectedAvailability();
    return available !== null && available <= 0;
  });
  protected readonly lowStock = computed(() => {
    const available = this.selectedAvailability();
    return available !== null && available > 0 && available < 10;
  });
  protected readonly canSubmit = computed(() => {
    const available = this.selectedAvailability();
    return !this.checkingAvailability() && available !== null && available > 0;
  });
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
      const eventID = params.get('event_id') ?? '';
      this.eventID.set(eventID);
      this.checkAvailability(eventID);
    });
  }

  protected changeEvent(event: Event): void {
    if (event.target instanceof HTMLSelectElement) {
      this.eventID.set(event.target.value);
      this.idempotencyKey = crypto.randomUUID();
      this.checkAvailability(event.target.value);
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
    if (this.checkingAvailability() || this.selectedAvailability() === null) {
      this.error.set('Espera a que se compruebe la disponibilidad del evento.');
      return;
    }
    if (this.selectedSoldOut()) {
      this.error.set('El evento está agotado. Selecciona otro evento.');
      return;
    }

    this.submitting.set(true);
    this.error.set(null);
    this.eventsService
      .getEvent(eventID)
      .pipe(
        switchMap((event) => {
          if (!this.setAvailability(event.available)) {
            this.submitting.set(false);
            return EMPTY;
          }
          if (event.available <= 0) {
            this.error.set('El evento se agotó. Selecciona otro evento.');
            this.submitting.set(false);
            return EMPTY;
          }
          return this.bookingsService.createBooking({
            event_id: eventID,
            quantity,
            idempotency_key: this.idempotencyKey
          });
        }),
        takeUntilDestroyed(this.destroyRef)
      )
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

  private checkAvailability(eventID: string): void {
    this.selectedAvailability.set(null);
    this.checkingAvailability.set(Boolean(eventID));
    if (!eventID) {
      return;
    }

    this.error.set(null);
    this.eventsService
      .getEvent(eventID)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (event) => {
          if (this.eventID() !== eventID) {
            return;
          }
          this.setAvailability(event.available);
          this.checkingAvailability.set(false);
        },
        error: (error: HttpErrorResponse) => {
          if (this.eventID() !== eventID) {
            return;
          }
          this.error.set(error.error?.error ?? 'No se pudo comprobar la disponibilidad.');
          this.checkingAvailability.set(false);
        }
      });
  }

  private setAvailability(available: number): boolean {
    if (!Number.isInteger(available)) {
      this.selectedAvailability.set(null);
      this.error.set(
        'La API no devolvió la disponibilidad del evento. Reinicia el backend y vuelve a intentarlo.'
      );
      return false;
    }
    this.selectedAvailability.set(available);
    return true;
  }
}
