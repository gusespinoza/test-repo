import { ComponentFixture, fakeAsync, TestBed, tick } from '@angular/core/testing';
import { ActivatedRoute, convertToParamMap, provideRouter } from '@angular/router';
import { of } from 'rxjs';

import { EventsService } from '../events/events.service';
import { BookingCheckoutComponent } from './booking-checkout.component';
import { Order } from './bookings.models';
import { BookingsService } from './bookings.service';

describe('BookingCheckoutComponent payment', () => {
  let fixture: ComponentFixture<BookingCheckoutComponent>;
  let confirmBooking: jasmine.Spy;
  let pendingBooking: Order;

  beforeEach(async () => {
    pendingBooking = {
      id: 'booking-1',
      user_id: 'user-1',
      event_id: 'event-1',
      quantity: 2,
      status: 'pending',
      idempotency_key: 'key-1',
      expires_at: new Date(Date.now() + 60_000).toISOString(),
      created_at: new Date().toISOString()
    };
    confirmBooking = jasmine.createSpy('confirmBooking').and.returnValue(
      of({ ...pendingBooking, status: 'confirmed' })
    );

    await TestBed.configureTestingModule({
      imports: [BookingCheckoutComponent],
      providers: [
        provideRouter([]),
        {
          provide: ActivatedRoute,
          useValue: { paramMap: of(convertToParamMap({ id: pendingBooking.id })) }
        },
        {
          provide: BookingsService,
          useValue: {
            getBooking: () => of(pendingBooking),
            confirmBooking,
            cancelBooking: () => of({ ...pendingBooking, status: 'cancelled' })
          }
        },
        {
          provide: EventsService,
          useValue: { getEvent: () => of({ available: 5 }) }
        }
      ]
    }).compileComponents();
  });

  it('waits two seconds before confirming and then shows purchase success', fakeAsync(() => {
    fixture = TestBed.createComponent(BookingCheckoutComponent);
    fixture.detectChanges();
    tick(0);
    fixture.detectChanges();

    const host = fixture.nativeElement as HTMLElement;
    (host.querySelector('.actions .primary-button') as HTMLButtonElement).click();
    fixture.detectChanges();

    expect(host.textContent).toContain('PASARELA DE PAGO SIMULADA');
    expect(confirmBooking).not.toHaveBeenCalled();

    (host.querySelector('#card-number') as HTMLInputElement).value = '0000000000000000';
    (host.querySelector('#card-expiration') as HTMLInputElement).value = '12/30';
    (host.querySelector('#card-cvc') as HTMLInputElement).value = '123';
    (host.querySelector('.payment-actions .primary-button') as HTMLButtonElement).click();
    fixture.detectChanges();

    expect(host.textContent).toContain('Procesando pago...');
    tick(1999);
    expect(confirmBooking).not.toHaveBeenCalled();

    tick(1);
    fixture.detectChanges();

    expect(confirmBooking).toHaveBeenCalledOnceWith(pendingBooking.id);
    expect(host.textContent).toContain('¡Compra realizada!');
    expect(host.querySelector('a[routerLink="/events"]')?.textContent).toContain('Volver al inicio');
  }));
});
