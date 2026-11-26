import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ActivatedRoute, convertToParamMap, provideRouter } from '@angular/router';
import { of } from 'rxjs';

import type { Event } from '../events/events.models';
import { EventsService } from '../events/events.service';
import { BookingSelectionComponent } from './booking-selection.component';
import { BookingsService } from './bookings.service';

describe('BookingSelectionComponent availability', () => {
  let fixture: ComponentFixture<BookingSelectionComponent>;
  let createBooking: jasmine.Spy;
  let eventID: string;
  let available: number | undefined;

  beforeEach(async () => {
    eventID = '00000000-0000-0000-0000-000000000001';
    available = 0;
    const event: Event = {
      id: eventID,
      title: 'Evento agotado',
      description: '',
      venue: 'Auditorio',
      starts_at: '2026-12-01T10:00:00Z',
      capacity: 20,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z'
    };
    createBooking = jasmine.createSpy('createBooking');

    await TestBed.configureTestingModule({
      imports: [BookingSelectionComponent],
      providers: [
        provideRouter([]),
        {
          provide: ActivatedRoute,
          useValue: {
            queryParamMap: of(convertToParamMap({}))
          }
        },
        {
          provide: EventsService,
          useValue: {
            listEvents: () => of({ items: [event], page: 1, limit: 100, total: 1 }),
            getEvent: () => of({ ...event, available })
          }
        },
        {
          provide: BookingsService,
          useValue: { createBooking }
        }
      ]
    }).compileComponents();
  });

  function renderWithSelectedEvent(): HTMLElement {
    fixture = TestBed.createComponent(BookingSelectionComponent);
    fixture.detectChanges();

    const host = fixture.nativeElement as HTMLElement;
    const select = host.querySelector('select') as HTMLSelectElement;
    select.value = eventID;
    select.dispatchEvent(new Event('change', { bubbles: true }));
    fixture.detectChanges();
    return host;
  }

  it('prevents creating an order for a sold-out event', () => {
    const host = renderWithSelectedEvent();
    const button = host.querySelector('button[type="submit"]') as HTMLButtonElement;
    expect(button.disabled).toBeTrue();
    expect(getComputedStyle(button).cursor).toBe('not-allowed');
    host.querySelector('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));

    expect(createBooking).not.toHaveBeenCalled();
  });

  it('enables checkout after selecting an event with available tickets', () => {
    available = 5;
    const host = renderWithSelectedEvent();

    expect((host.querySelector('button[type="submit"]') as HTMLButtonElement).disabled).toBeFalse();
  });

  it('explains when the API response is missing availability', () => {
    available = undefined;
    const host = renderWithSelectedEvent();

    expect(host.querySelector('[role="alert"]')?.textContent).toContain('Reinicia el backend');
    expect((host.querySelector('button[type="submit"]') as HTMLButtonElement).disabled).toBeTrue();
  });
});
